package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Project roles (K60). A member or an agent may carry a role on a project
// (viewer, contributor, admin) that only restricts what the workspace role
// already grants: a workspace owner or admin is a project admin by default,
// a member a contributor, an agent a contributor; a row may lower that,
// never raise it. Without a row the workspace role applies, silently.

const (
	ProjectRoleViewer      = "viewer"
	ProjectRoleContributor = "contributor"
	ProjectRoleAdmin       = "admin"
	AuditProjectRoleSet    = "project.role_set"

	// ErrCodeProjectRoleForbidden marks a batch item refused for a
	// project-role mismatch, as opposed to a transition/gate refusal.
	ErrCodeProjectRoleForbidden = "project_role_forbidden"
)

var projectRoleRank = map[string]int{ProjectRoleViewer: 0, ProjectRoleContributor: 1, ProjectRoleAdmin: 2}

// projectRoleCeiling is the most a subject can hold on any project.
func projectRoleCeiling(subjectType, workspaceRole string) string {
	if subjectType == "agent" {
		return ProjectRoleContributor
	}
	if roleAllowed(workspaceRole, "owner", "admin") {
		return ProjectRoleAdmin
	}
	return ProjectRoleContributor
}

// effectiveProjectRole is the role in force: the override when it is lower
// than the ceiling, the ceiling otherwise.
func (h *Handler) effectiveProjectRole(ctx context.Context, projectID pgtype.UUID, subjectType string, subjectID pgtype.UUID, workspaceRole string) (role string, overridden bool) {
	ceiling := projectRoleCeiling(subjectType, workspaceRole)
	row, err := h.Queries.GetProjectMemberRole(ctx, db.GetProjectMemberRoleParams{ProjectID: projectID, SubjectType: subjectType, SubjectID: subjectID})
	if err != nil {
		return ceiling, false
	}
	if projectRoleRank[row.Role] < projectRoleRank[ceiling] {
		return row.Role, true
	}
	return ceiling, true
}

// resolveProjectRole is the response-free core of requireProjectRole: the
// acting subject's effective role on the project, or an error when the
// caller has no membership row at all. The subject is the resolved actor:
// an agent acting through its run token is judged by its own project role,
// not by its owner's. Callers must check projectID.Valid themselves — an
// invalid project has no role to resolve.
func (h *Handler) resolveProjectRole(r *http.Request, projectID pgtype.UUID) (string, error) {
	workspaceID := h.resolveWorkspaceID(r)
	userID := requestUserID(r)
	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(userID), WorkspaceID: parseUUID(workspaceID)})
	if err != nil {
		return "", err
	}
	subjectType, subjectID := "member", member.ID
	if actorType, actorID := h.resolveActor(r, userID, workspaceID); actorType == "agent" && actorID != "" {
		subjectType, subjectID = "agent", parseUUID(actorID)
	}
	role, _ := h.effectiveProjectRole(r.Context(), projectID, subjectType, subjectID, member.Role)
	return role, nil
}

// requireProjectRole refuses the request when the acting subject's role on
// the project is below `min`. A null project always passes.
func (h *Handler) requireProjectRole(w http.ResponseWriter, r *http.Request, projectID pgtype.UUID, min string) bool {
	if !projectID.Valid {
		return true
	}
	role, err := h.resolveProjectRole(r, projectID)
	if err != nil {
		writeError(w, http.StatusForbidden, "no access to this project")
		return false
	}
	if projectRoleRank[role] < projectRoleRank[min] {
		writeError(w, http.StatusForbidden, "your project role ("+role+") does not allow this")
		return false
	}
	return true
}

// requireProjectRoleManager gates changing a project's roles. A workspace
// owner or admin, acting as themselves, always passes: the workspace role is
// what grants every ceiling, and gating them on their project role would let
// one who restricted their own role lock themselves (or be locked) out of
// undoing it. Anyone else needs to be a project admin.
func (h *Handler) requireProjectRoleManager(w http.ResponseWriter, r *http.Request, projectID pgtype.UUID) bool {
	workspaceID, userID := h.resolveWorkspaceID(r), requestUserID(r)
	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(userID), WorkspaceID: parseUUID(workspaceID)})
	if err == nil && roleAllowed(member.Role, "owner", "admin") {
		if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType != "agent" {
			return true
		}
	}
	return h.requireProjectRole(w, r, projectID, ProjectRoleAdmin)
}

// requireProjectWrite is the gate on issue and resource writes.
func (h *Handler) requireProjectWrite(w http.ResponseWriter, r *http.Request, projectID pgtype.UUID) bool {
	return h.requireProjectRole(w, r, projectID, ProjectRoleContributor)
}

// projectWriteAllowed is requireProjectWrite without writing an HTTP
// response — for a batch loop that must refuse/skip a single item rather
// than abort the whole request on one project-role mismatch.
func (h *Handler) projectWriteAllowed(r *http.Request, projectID pgtype.UUID) bool {
	if !projectID.Valid {
		return true
	}
	role, err := h.resolveProjectRole(r, projectID)
	if err != nil {
		return false
	}
	return projectRoleRank[role] >= projectRoleRank[ProjectRoleContributor]
}

// projectWriteAllowedForActor is projectWriteAllowed for a subject already
// resolved by the caller instead of the ordinary session/task-token request
// headers — the plugin-action bridge, where a callback token's actor comes
// from a signed grant, not from X-User-ID. An empty subjectType (a pure
// plugin install-token call with no member/agent behind it) has no project
// role to check and passes: K60 governs members and agents, not a plugin's
// own identity.
func (h *Handler) projectWriteAllowedForActor(r *http.Request, projectID pgtype.UUID, subjectType string, subjectID pgtype.UUID, workspaceRole string) bool {
	if !projectID.Valid || subjectType == "" {
		return true
	}
	role, _ := h.effectiveProjectRole(r.Context(), projectID, subjectType, subjectID, workspaceRole)
	return projectRoleRank[role] >= projectRoleRank[ProjectRoleContributor]
}

type ProjectMemberRoleResponse struct {
	SubjectType   string  `json:"subject_type"`
	SubjectID     string  `json:"subject_id"`
	Name          string  `json:"name"`
	Email         string  `json:"email,omitempty"`
	WorkspaceRole string  `json:"workspace_role"`
	Ceiling       string  `json:"ceiling"`
	EffectiveRole string  `json:"effective_role"`
	Source        string  `json:"source"`
	Override      *string `json:"override"`
}

func (h *Handler) loadProjectForRoles(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return db.Project{}, false
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: parseUUID(workspaceID)})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

// GET /api/projects/{id}/members — every member and agent with the role in force.
func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForRoles(w, r)
	if !ok {
		return
	}
	overrides := map[string]db.ProjectMemberRole{}
	if rows, err := h.Queries.ListProjectMemberRoles(r.Context(), project.ID); err == nil {
		for _, row := range rows {
			overrides[row.SubjectType+":"+uuidToString(row.SubjectID)] = row
		}
	}
	entry := func(subjectType string, subjectID pgtype.UUID, name, email, workspaceRole string) ProjectMemberRoleResponse {
		ceiling := projectRoleCeiling(subjectType, workspaceRole)
		out := ProjectMemberRoleResponse{SubjectType: subjectType, SubjectID: uuidToString(subjectID), Name: name, Email: email, WorkspaceRole: workspaceRole, Ceiling: ceiling, EffectiveRole: ceiling, Source: "inherited"}
		if row, ok := overrides[subjectType+":"+uuidToString(subjectID)]; ok {
			role := row.Role
			out.Override = &role
			out.Source = "override"
			if projectRoleRank[row.Role] < projectRoleRank[ceiling] {
				out.EffectiveRole = row.Role
			}
		}
		return out
	}
	members := []ProjectMemberRoleResponse{}
	if rows, err := h.Queries.ListMembersWithUser(r.Context(), project.WorkspaceID); err == nil {
		for _, m := range rows {
			members = append(members, entry("member", m.ID, m.UserName, m.UserEmail, m.Role))
		}
	}
	if rows, err := h.Queries.ListAgents(r.Context(), project.WorkspaceID); err == nil {
		for _, a := range rows {
			if a.ArchivedAt.Valid {
				continue
			}
			members = append(members, entry("agent", a.ID, a.Name, "", "agent"))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members, "roles": []string{ProjectRoleViewer, ProjectRoleContributor, ProjectRoleAdmin}})
}

// PUT /api/projects/{id}/members/{subjectType}/{subjectId}/role {role}
// Workspace owner/admin or a project admin; the role may not exceed the
// subject's ceiling.
func (h *Handler) SetProjectMemberRole(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForRoles(w, r)
	if !ok {
		return
	}
	if !h.requireProjectRoleManager(w, r, project.ID) {
		return
	}
	subjectType := chi.URLParam(r, "subjectType")
	subjectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "subjectId"), "subject id")
	if !ok {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role := strings.TrimSpace(req.Role)
	if _, known := projectRoleRank[role]; !known {
		writeError(w, http.StatusBadRequest, "role must be viewer, contributor or admin")
		return
	}
	var workspaceRole string
	switch subjectType {
	case "member":
		m, err := h.Queries.GetMemberByID(r.Context(), db.GetMemberByIDParams{ID: subjectID, WorkspaceID: project.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusNotFound, "member not found")
			return
		}
		workspaceRole = m.Role
	case "agent":
		a, err := h.Queries.GetAgent(r.Context(), subjectID)
		if err != nil || a.WorkspaceID != project.WorkspaceID {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		workspaceRole = "agent"
	default:
		writeError(w, http.StatusBadRequest, "subject type must be member or agent")
		return
	}
	if ceiling := projectRoleCeiling(subjectType, workspaceRole); projectRoleRank[role] > projectRoleRank[ceiling] {
		writeError(w, http.StatusBadRequest, "a project role cannot exceed the subject's workspace role ("+ceiling+" at most)")
		return
	}
	if _, err := h.Queries.UpsertProjectMemberRole(r.Context(), db.UpsertProjectMemberRoleParams{ID: dbid.NewV7(), WorkspaceID: project.WorkspaceID, ProjectID: project.ID, SubjectType: subjectType, SubjectID: subjectID, Role: role, CreatedBy: parseUUID(requestUserID(r))}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the role")
		return
	}
	h.audit(r.Context(), project.WorkspaceID, "member", requestUserID(r), AuditProjectRoleSet, "project", project.ID, map[string]any{"subject_type": subjectType, "subject_id": uuidToString(subjectID), "role": role}, nil)
	h.ListProjectMembers(w, r)
}

// DELETE /api/projects/{id}/members/{subjectType}/{subjectId}/role — back to inherited.
func (h *Handler) ClearProjectMemberRole(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForRoles(w, r)
	if !ok {
		return
	}
	if !h.requireProjectRoleManager(w, r, project.ID) {
		return
	}
	subjectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "subjectId"), "subject id")
	if !ok {
		return
	}
	if _, err := h.Queries.DeleteProjectMemberRole(r.Context(), db.DeleteProjectMemberRoleParams{ProjectID: project.ID, SubjectType: chi.URLParam(r, "subjectType"), SubjectID: subjectID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear the role")
		return
	}
	h.audit(r.Context(), project.WorkspaceID, "member", requestUserID(r), AuditProjectRoleSet, "project", project.ID, map[string]any{"subject_type": chi.URLParam(r, "subjectType"), "subject_id": uuidToString(subjectID), "role": "inherited"}, nil)
	h.ListProjectMembers(w, r)
}
