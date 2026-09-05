package handler

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Run confidence scoring (JEF-240): the workspace policy behind the automatic
// human-review escalation of below-threshold runs.

const AuditConfidenceReviewSettingsChanged = "confidence_review.settings_changed"

// GetConfidenceReviewSettings: GET /api/confidence-review-settings — the
// workspace's confidence_review settings, with defaults filled in.
func (h *Handler) GetConfidenceReviewSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.ConfidenceReviewSettings(ws.Settings))
}

// PutConfidenceReviewSettings: PUT /api/confidence-review-settings {enabled, threshold}.
func (h *Handler) PutConfidenceReviewSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.ConfidenceReview
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !service.ValidConfidenceThreshold(req.Threshold) {
		writeError(w, http.StatusBadRequest, "threshold must be greater than 0 and at most 1")
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	settings := map[string]any{}
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &settings)
	}
	next := service.ConfidenceReview{Enabled: req.Enabled, Threshold: req.Threshold}
	settings["confidence_review"] = next
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save confidence review settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditConfidenceReviewSettingsChanged, "workspace", wsUUID, map[string]any{"enabled": next.Enabled, "threshold": next.Threshold}, nil)
	writeJSON(w, http.StatusOK, next)
}
