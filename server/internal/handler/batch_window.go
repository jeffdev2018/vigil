package handler

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"log/slog"
)

// Off-peak batch lane (K45). The workspace declares one window during which
// autopilots marked batch_eligible are dispatched into the batch lane, behind
// every synchronous task of the same agent/runtime. These endpoints only read
// and write the declaration; service/autopilot.go is what applies it, and
// agent.sql's claim ordering is what enforces it.
//
// The window lives in workspace.settings.batch_window, so it needs no table
// and is purged with the workspace.

// AuditBatchWindow records a change to the workspace off-peak window.
const AuditBatchWindow = "batch_window.policy"

// GetBatchWindow: GET /api/batch-window.
func (h *Handler) GetBatchWindow(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.BatchWindowFromSettings(ws.Settings))
}

// PutBatchWindow: PUT /api/batch-window
// {enabled, start_local_time, end_local_time, timezone}.
//
// Admin-only: the window decides when a whole workspace's non-urgent work is
// allowed to wait, which is not one autopilot owner's call.
func (h *Handler) PutBatchWindow(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.BatchWindow
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	window := service.NormalizeBatchWindow(req)
	if err := service.ValidateBatchWindow(window); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	settings := map[string]any{}
	if len(ws.Settings) > 0 {
		if err := json.Unmarshal(ws.Settings, &settings); err != nil {
			// Writing over a blob we could not read would erase every other
			// workspace setting; refuse instead.
			slog.Error("batch window: workspace settings unreadable", "workspace_id", uuidToString(wsUUID), "error", err)
			writeError(w, http.StatusInternalServerError, "workspace settings are unreadable")
			return
		}
	}
	settings["batch_window"] = window
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the off-peak window")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditBatchWindow, "workspace", wsUUID, map[string]any{
		"enabled":          window.Enabled,
		"start_local_time": window.Start,
		"end_local_time":   window.End,
		"timezone":         window.Timezone,
	}, nil)
	writeJSON(w, http.StatusOK, window)
}
