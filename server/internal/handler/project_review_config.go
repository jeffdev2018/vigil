package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Per-project review policy (JEF-238): a checklist the cross-provider
// reviewer must verify, an optional pinned reviewer agent, and a done gate
// that loops a request_changes verdict back to the worker, bounded by
// max_cycles. Without a row the project keeps the K15 defaults: no checklist,
// automatic reviewer choice, no gate.

const (
	AuditProjectReviewConfigChanged = "project_review_config.changed"
	reviewConfigMaxChecklistItems   = 20
	reviewConfigMaxCycles           = 10
	reviewConfigDefaultMaxCycles    = 3
)

type ProjectReviewConfigResponse struct {
	ProjectID       string   `json:"project_id"`
	Checklist       []string `json:"checklist"`
	ReviewerAgentID *string  `json:"reviewer_agent_id"`
	GateEnabled     bool     `json:"gate_enabled"`
	MaxCycles       int      `json:"max_cycles"`
}

// defaultProjectReviewConfig is what a project without a row behaves like.
func defaultProjectReviewConfig(projectID string) ProjectReviewConfigResponse {
	return ProjectReviewConfigResponse{ProjectID: projectID, Checklist: []string{}, ReviewerAgentID: nil, GateEnabled: false, MaxCycles: reviewConfigDefaultMaxCycles}
}

func projectReviewConfigToResponse(cfg db.ProjectReviewConfig) ProjectReviewConfigResponse {
	resp := defaultProjectReviewConfig(uuidToString(cfg.ProjectID))
	_ = json.Unmarshal(cfg.Checklist, &resp.Checklist)
	if resp.Checklist == nil {
		resp.Checklist = []string{}
	}
	if cfg.ReviewerAgentID.Valid {
		id := uuidToString(cfg.ReviewerAgentID)
		resp.ReviewerAgentID = &id
	}
	resp.GateEnabled = cfg.GateEnabled
	resp.MaxCycles = int(cfg.MaxCycles)
	return resp
}

// loadProjectForReviewConfig resolves the project in the request workspace;
// the GET/PUT pair shares the 400/404 ladder of the other project endpoints.
func (h *Handler) loadProjectForReviewConfig(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

// GetProjectReviewConfig: GET /api/projects/{id}/review-config — the saved
// row, or the defaults when the project never configured one (never 404 for
// a real project).
func (h *Handler) GetProjectReviewConfig(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForReviewConfig(w, r)
	if !ok {
		return
	}
	cfg, err := h.Queries.GetProjectReviewConfig(r.Context(), project.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, defaultProjectReviewConfig(uuidToString(project.ID)))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project review config")
		return
	}
	writeJSON(w, http.StatusOK, projectReviewConfigToResponse(cfg))
}

type putProjectReviewConfigRequest struct {
	Checklist       []string `json:"checklist"`
	ReviewerAgentID *string  `json:"reviewer_agent_id"`
	GateEnabled     bool     `json:"gate_enabled"`
	MaxCycles       *int     `json:"max_cycles"`
}

// PutProjectReviewConfig: PUT /api/projects/{id}/review-config — upsert.
func (h *Handler) PutProjectReviewConfig(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForReviewConfig(w, r)
	if !ok {
		return
	}
	var req putProjectReviewConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	maxCycles := reviewConfigDefaultMaxCycles
	if req.MaxCycles != nil {
		maxCycles = *req.MaxCycles
	}
	if maxCycles < 1 || maxCycles > reviewConfigMaxCycles {
		writeError(w, http.StatusBadRequest, "max_cycles must be between 1 and 10")
		return
	}
	if len(req.Checklist) > reviewConfigMaxChecklistItems {
		writeError(w, http.StatusBadRequest, "checklist must have at most 20 items")
		return
	}
	checklist := make([]string, 0, len(req.Checklist))
	for _, item := range req.Checklist {
		item = strings.TrimSpace(item)
		if item == "" {
			writeError(w, http.StatusBadRequest, "checklist items must be non-empty")
			return
		}
		checklist = append(checklist, item)
	}
	reviewerID := pgtype.UUID{}
	if req.ReviewerAgentID != nil && strings.TrimSpace(*req.ReviewerAgentID) != "" {
		parsed, ok := parseUUIDOrBadRequest(w, *req.ReviewerAgentID, "reviewer agent id")
		if !ok {
			return
		}
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: parsed, WorkspaceID: project.WorkspaceID})
		if err != nil || agent.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, "reviewer_agent_id must be a live agent of this workspace")
			return
		}
		reviewerID = parsed
	}
	checklistJSON, _ := json.Marshal(checklist)
	cfg, err := h.Queries.UpsertProjectReviewConfig(r.Context(), db.UpsertProjectReviewConfigParams{
		ProjectID:       project.ID,
		WorkspaceID:     project.WorkspaceID,
		Checklist:       checklistJSON,
		ReviewerAgentID: reviewerID,
		GateEnabled:     req.GateEnabled,
		MaxCycles:       int32(maxCycles),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save project review config")
		return
	}
	h.audit(r.Context(), project.WorkspaceID, "member", requestUserID(r), AuditProjectReviewConfigChanged, "project", project.ID, map[string]any{"gate_enabled": req.GateEnabled, "max_cycles": maxCycles, "checklist_items": len(checklist), "reviewer_agent_id": uuidToString(reviewerID)}, nil)
	writeJSON(w, http.StatusOK, projectReviewConfigToResponse(cfg))
}
