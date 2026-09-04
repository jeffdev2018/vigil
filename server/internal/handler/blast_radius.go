package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/blastradius"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Blast radius (K07): per-project rules (path pattern → autonomous,
// read_only, dual_approval) that the approval gates (K05) consult for the
// paths an action touches. No rule for a path means the agent's own
// permissions decide, as before.

type BlastRadiusRuleResponse struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	PathPattern string `json:"path_pattern"`
	Level       string `json:"autonomy_level"`
	Specificity int    `json:"specificity"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

func blastRuleToResponse(r db.ProjectBlastRadiusRule) BlastRadiusRuleResponse {
	return BlastRadiusRuleResponse{
		ID: uuidToString(r.ID), ProjectID: uuidToString(r.ProjectID), PathPattern: r.PathPattern, Level: r.AutonomyLevel,
		Specificity: blastradius.Specificity(r.PathPattern), CreatedBy: uuidToString(r.CreatedBy), CreatedAt: timestampToString(r.CreatedAt),
	}
}

func toBlastRules(rows []db.ProjectBlastRadiusRule) []blastradius.Rule {
	out := make([]blastradius.Rule, 0, len(rows))
	for _, r := range rows {
		out = append(out, blastradius.Rule{ID: uuidToString(r.ID), Pattern: r.PathPattern, Level: r.AutonomyLevel})
	}
	return out
}

// projectBlastRules loads a project's rules; an issue without project has none.
func (h *Handler) projectBlastRules(ctx context.Context, wsID, projectID pgtype.UUID) []blastradius.Rule {
	if !projectID.Valid {
		return nil
	}
	rows, err := h.Queries.ListBlastRadiusRules(ctx, db.ListBlastRadiusRulesParams{WorkspaceID: wsID, ProjectID: projectID})
	if err != nil {
		return nil
	}
	return toBlastRules(rows)
}

func (h *Handler) blastProject(w http.ResponseWriter, r *http.Request, roles ...string) (pgtype.UUID, pgtype.UUID, string, bool) {
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, "", false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, "", false
	}
	if len(roles) == 0 {
		if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
			return pgtype.UUID{}, pgtype.UUID{}, "", false
		}
	} else if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", roles...); !ok {
		return pgtype.UUID{}, pgtype.UUID{}, "", false
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return pgtype.UUID{}, pgtype.UUID{}, "", false
	}
	return wsUUID, projectID, userID, true
}

// GET /api/projects/{id}/blast-radius-rules — most specific first.
func (h *Handler) ListBlastRadiusRules(w http.ResponseWriter, r *http.Request) {
	wsUUID, projectID, _, ok := h.blastProject(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListBlastRadiusRules(r.Context(), db.ListBlastRadiusRulesParams{WorkspaceID: wsUUID, ProjectID: projectID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	out := make([]BlastRadiusRuleResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, blastRuleToResponse(row))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Specificity > out[j].Specificity })
	writeJSON(w, http.StatusOK, map[string]any{"rules": out, "levels": blastradius.Levels})
}

// POST /api/projects/{id}/blast-radius-rules {path_pattern, autonomy_level} (owner/admin)
func (h *Handler) CreateBlastRadiusRule(w http.ResponseWriter, r *http.Request) {
	wsUUID, projectID, userID, ok := h.blastProject(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req struct {
		PathPattern string `json:"path_pattern"`
		Level       string `json:"autonomy_level"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.PathPattern = strings.Trim(strings.TrimSpace(req.PathPattern), "/")
	if _, err := blastradius.Compile(req.PathPattern); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeInvalidPathPattern, err.Error())
		return
	}
	if !blastradius.ValidLevel(req.Level) {
		writeError(w, http.StatusBadRequest, "autonomy_level must be one of "+strings.Join(blastradius.Levels, ", "))
		return
	}
	existing, err := h.Queries.ListBlastRadiusRules(r.Context(), db.ListBlastRadiusRulesParams{WorkspaceID: wsUUID, ProjectID: projectID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	for _, e := range existing {
		if e.PathPattern == req.PathPattern {
			writeErrorCode(w, http.StatusConflict, "blast_radius_duplicate", "a rule already covers exactly this pattern")
			return
		}
	}
	if other, conflict := blastradius.Conflicts(toBlastRules(existing), blastradius.Rule{Pattern: req.PathPattern, Level: req.Level}); conflict {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "blast_radius_conflict", "error": "a rule of the same specificity gives " + other.Level + " to the same paths: " + other.Pattern, "conflicting_rule_id": other.ID})
		return
	}
	row, err := h.Queries.CreateBlastRadiusRule(r.Context(), db.CreateBlastRadiusRuleParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, ProjectID: projectID, PathPattern: req.PathPattern, AutonomyLevel: req.Level, CreatedBy: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the rule")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, "blast_radius_rule.created", "project", projectID, map[string]any{"pattern": req.PathPattern, "level": req.Level}, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"rule": blastRuleToResponse(row)})
}

// DELETE /api/projects/{id}/blast-radius-rules/{ruleId} (owner/admin)
func (h *Handler) DeleteBlastRadiusRule(w http.ResponseWriter, r *http.Request) {
	wsUUID, projectID, userID, ok := h.blastProject(w, r, "owner", "admin")
	if !ok {
		return
	}
	ruleID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "ruleId"), "rule id")
	if !ok {
		return
	}
	n, err := h.Queries.DeleteBlastRadiusRule(r.Context(), db.DeleteBlastRadiusRuleParams{ID: ruleID, WorkspaceID: wsUUID, ProjectID: projectID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the rule")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, "blast_radius_rule.deleted", "project", projectID, map[string]any{"rule_id": uuidToString(ruleID)}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/projects/{id}/blast-radius-preview?path= — which rule would decide.
func (h *Handler) PreviewBlastRadius(w http.ResponseWriter, r *http.Request) {
	wsUUID, projectID, _, ok := h.blastProject(w, r)
	if !ok {
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	rules := h.projectBlastRules(r.Context(), wsUUID, projectID)
	rule, found := blastradius.Resolve(rules, path)
	out := map[string]any{"path": path, "level": "inherit"}
	if found {
		out["level"] = rule.Level
		out["rule_id"] = rule.ID
		out["path_pattern"] = rule.Pattern
	}
	writeJSON(w, http.StatusOK, out)
}
