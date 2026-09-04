package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/permissionprofile"
)

// Permission profiles (K06): what an agent may touch when it runs. Five
// builtin profiles are seeded per workspace on first read. An agent carries
// one; a run may override it (admin). The resolved profile leaves with the
// claim payload and is enforced at the approval gates here.

const (
	AuditPermissionProfileChanged  = "permission_profile.changed"
	AuditPermissionProfileAssigned = "permission_profile.assigned"
)

type PermissionProfileRequest struct {
	Name            string   `json:"name"`
	Description     *string  `json:"description"`
	ReadOnly        *bool    `json:"read_only"`
	DeniedPaths     []string `json:"denied_paths"`
	AllowedCommands []string `json:"allowed_commands"`
	HiddenSecrets   []string `json:"hidden_secrets"`
}

func jsonStrings(raw []byte) []string {
	out := []string{}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func profileFromRow(row db.AgentPermissionProfile) permissionprofile.Profile {
	return permissionprofile.Profile{
		ID: uuidToString(row.ID), Name: row.Name, Description: row.Description, ReadOnly: row.ReadOnly, Builtin: row.Builtin,
		DeniedPaths: jsonStrings(row.DeniedPaths), AllowedCommands: jsonStrings(row.AllowedCommands), HiddenSecrets: jsonStrings(row.HiddenSecrets),
	}
}

func profileJSON(list []string) []byte {
	if list == nil {
		list = []string{}
	}
	raw, _ := json.Marshal(list)
	return raw
}

// ensurePermissionProfiles seeds the builtin profiles the first time a
// workspace asks for them. A concurrent seed loses on the unique index and
// simply reads what the other wrote.
func (h *Handler) ensurePermissionProfiles(ctx context.Context, wsID pgtype.UUID) ([]db.AgentPermissionProfile, error) {
	rows, err := h.Queries.ListPermissionProfiles(ctx, wsID)
	if err != nil || len(rows) > 0 {
		return rows, err
	}
	for _, p := range permissionprofile.Defaults() {
		_, err := h.Queries.CreatePermissionProfile(ctx, db.CreatePermissionProfileParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, Name: p.Name, Description: p.Description, ReadOnly: p.ReadOnly, Builtin: true,
			DeniedPaths: profileJSON(p.DeniedPaths), AllowedCommands: profileJSON(p.AllowedCommands), HiddenSecrets: profileJSON(p.HiddenSecrets),
		})
		var pgErr *pgconn.PgError
		if err != nil && !(errors.As(err, &pgErr) && pgErr.Code == "23505") {
			return nil, err
		}
	}
	return h.Queries.ListPermissionProfiles(ctx, wsID)
}

// taskPermissionProfile resolves the profile a run works under: the run's
// override first, else its agent's. agent may be nil (loaded then).
func (h *Handler) taskPermissionProfile(ctx context.Context, task db.AgentTaskQueue, agent *db.Agent) (permissionprofile.Profile, string, bool) {
	id, source := task.PermissionProfileID, "task"
	if !id.Valid {
		if agent == nil {
			loaded, err := h.Queries.GetAgent(ctx, task.AgentID)
			if err != nil {
				return permissionprofile.Profile{}, "", false
			}
			agent = &loaded
		}
		id, source = agent.PermissionProfileID, "agent"
	}
	if !id.Valid {
		return permissionprofile.Profile{}, "none", false
	}
	row, err := h.Queries.GetPermissionProfile(ctx, id)
	if err != nil {
		return permissionprofile.Profile{}, "", false
	}
	return profileFromRow(row), source, true
}

func (h *Handler) permissionProfileScope(w http.ResponseWriter, r *http.Request, roles ...string) (pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if len(roles) == 0 {
		_, ok = h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	} else {
		_, ok = h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", roles...)
	}
	return wsUUID, ok
}

func (h *Handler) loadPermissionProfile(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.AgentPermissionProfile, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "profile id")
	if !ok {
		return db.AgentPermissionProfile{}, false
	}
	row, err := h.Queries.GetPermissionProfile(r.Context(), id)
	if err != nil || row.WorkspaceID != wsUUID {
		writeError(w, http.StatusNotFound, "permission profile not found")
		return db.AgentPermissionProfile{}, false
	}
	return row, true
}

func (h *Handler) ListPermissionProfiles(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	rows, err := h.ensurePermissionProfiles(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list permission profiles")
		return
	}
	out := make([]permissionprofile.Profile, 0, len(rows))
	for _, row := range rows {
		out = append(out, profileFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
}

func (h *Handler) CreatePermissionProfile(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req PermissionProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p := permissionprofile.Profile{Name: strings.TrimSpace(req.Name), DeniedPaths: req.DeniedPaths, AllowedCommands: req.AllowedCommands, HiddenSecrets: req.HiddenSecrets}
	if req.Description != nil {
		p.Description = strings.TrimSpace(*req.Description)
	}
	if req.ReadOnly != nil {
		p.ReadOnly = *req.ReadOnly
	}
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	row, err := h.Queries.CreatePermissionProfile(r.Context(), db.CreatePermissionProfileParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Name: p.Name, Description: p.Description, ReadOnly: p.ReadOnly,
		DeniedPaths: profileJSON(p.DeniedPaths), AllowedCommands: profileJSON(p.AllowedCommands), HiddenSecrets: profileJSON(p.HiddenSecrets),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a profile with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create permission profile")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditPermissionProfileChanged, "permission_profile", row.ID, map[string]any{"name": row.Name, "created": true}, nil)
	writeJSON(w, http.StatusCreated, profileFromRow(row))
}

func (h *Handler) UpdatePermissionProfile(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	row, ok := h.loadPermissionProfile(w, r, wsUUID)
	if !ok {
		return
	}
	var req PermissionProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p := profileFromRow(row)
	if req.Description != nil {
		p.Description = strings.TrimSpace(*req.Description)
	}
	if req.ReadOnly != nil {
		p.ReadOnly = *req.ReadOnly
	}
	if req.DeniedPaths != nil {
		p.DeniedPaths = req.DeniedPaths
	}
	if req.AllowedCommands != nil {
		p.AllowedCommands = req.AllowedCommands
	}
	if req.HiddenSecrets != nil {
		p.HiddenSecrets = req.HiddenSecrets
	}
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.Queries.UpdatePermissionProfileRules(r.Context(), db.UpdatePermissionProfileRulesParams{
		ID: row.ID, Description: p.Description, ReadOnly: p.ReadOnly,
		DeniedPaths: profileJSON(p.DeniedPaths), AllowedCommands: profileJSON(p.AllowedCommands), HiddenSecrets: profileJSON(p.HiddenSecrets),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update permission profile")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditPermissionProfileChanged, "permission_profile", row.ID,
		map[string]any{"name": row.Name, "read_only": p.ReadOnly, "denied_paths": p.DeniedPaths, "allowed_commands": p.AllowedCommands, "hidden_secrets": p.HiddenSecrets}, nil)
	writeJSON(w, http.StatusOK, profileFromRow(updated))
}

func (h *Handler) DeletePermissionProfile(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	row, ok := h.loadPermissionProfile(w, r, wsUUID)
	if !ok {
		return
	}
	if row.Builtin {
		writeError(w, http.StatusBadRequest, "builtin profiles cannot be deleted; edit their rules instead")
		return
	}
	if n, err := h.Queries.CountAgentsUsingPermissionProfile(r.Context(), pgtype.UUID{Bytes: row.ID.Bytes, Valid: true}); err != nil || n > 0 {
		writeError(w, http.StatusConflict, "agents still use this profile")
		return
	}
	if _, err := h.Queries.DeletePermissionProfile(r.Context(), row.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete permission profile")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditPermissionProfileChanged, "permission_profile", row.ID, map[string]any{"name": row.Name, "deleted": true}, nil)
	w.WriteHeader(http.StatusNoContent)
}

type assignProfileRequest struct {
	ProfileID *string `json:"profile_id"`
}

// resolveAssignedProfile turns the request into a nullable id in this workspace.
func (h *Handler) resolveAssignedProfile(w http.ResponseWriter, r *http.Request, wsID pgtype.UUID) (pgtype.UUID, bool) {
	var req assignProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return pgtype.UUID{}, false
	}
	if req.ProfileID == nil || strings.TrimSpace(*req.ProfileID) == "" {
		return pgtype.UUID{}, true
	}
	id, ok := parseUUIDOrBadRequest(w, *req.ProfileID, "profile_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	row, err := h.Queries.GetPermissionProfile(r.Context(), id)
	if err != nil || row.WorkspaceID != wsID {
		writeError(w, http.StatusNotFound, "permission profile not found")
		return pgtype.UUID{}, false
	}
	return row.ID, true
}

// SetAgentPermissionProfile: PUT /api/agents/{id}/permission-profile {profile_id|null}.
func (h *Handler) SetAgentPermissionProfile(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return
	}
	profileID, ok := h.resolveAssignedProfile(w, r, agent.WorkspaceID)
	if !ok {
		return
	}
	updated, err := h.Queries.SetAgentPermissionProfile(r.Context(), db.SetAgentPermissionProfileParams{ID: agent.ID, PermissionProfileID: profileID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign permission profile")
		return
	}
	h.audit(r.Context(), agent.WorkspaceID, "member", requestUserID(r), AuditPermissionProfileAssigned, "agent", agent.ID, map[string]any{"profile_id": uuidToPtr(profileID)}, nil)
	writeJSON(w, http.StatusOK, h.agentToResponse(updated))
}

// GetTaskPermissionProfile: what this run works under, and where it came from.
func (h *Handler) GetTaskPermissionProfile(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	profile, source, found := h.taskPermissionProfile(r.Context(), task, nil)
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"profile": nil, "source": "none"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile, "source": source})
}

// SetTaskPermissionProfile: an admin override for one run.
func (h *Handler) SetTaskPermissionProfile(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "a run cannot change its own permissions")
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(agent.WorkspaceID), "task not found", "owner", "admin"); !ok {
		return
	}
	profileID, ok := h.resolveAssignedProfile(w, r, agent.WorkspaceID)
	if !ok {
		return
	}
	updated, err := h.Queries.SetAgentTaskPermissionProfile(r.Context(), db.SetAgentTaskPermissionProfileParams{ID: task.ID, PermissionProfileID: profileID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign permission profile")
		return
	}
	h.audit(r.Context(), agent.WorkspaceID, "member", requestUserID(r), AuditPermissionProfileAssigned, "task", task.ID, map[string]any{"profile_id": uuidToPtr(profileID)}, nil)
	profile, source, found := h.taskPermissionProfile(r.Context(), updated, &agent)
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"profile": nil, "source": "none"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile, "source": source})
}
