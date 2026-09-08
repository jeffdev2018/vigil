package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AuditRunHaltChanged records who stopped or restarted a workspace's agents.
const AuditRunHaltChanged = "run_halt.changed"

// GetRunHalt: GET /api/run-halt — whether this workspace's agents are held,
// who held them and why. Readable by every member: someone whose run was
// refused has to be able to find out by whom.
func (h *Handler) GetRunHalt(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.RunHaltFromSettings(ws.Settings))
}

// PutRunHalt: PUT /api/run-halt {halted, reason}. Owner or admin only, and
// audited both ways — lifting a halt is as much a decision as setting one.
func (h *Handler) PutRunHalt(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req struct {
		Halted bool   `json:"halted"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(req.Reason) > service.RunHaltMaxReasonRunes {
		writeError(w, http.StatusBadRequest, "reason is too long")
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
	next := service.RunHalt{Halted: req.Halted, Reason: req.Reason}
	if next.Halted {
		next.HaltedBy = requestUserID(r)
		next.HaltedAt = time.Now().UTC().Format(time.RFC3339)
	} else {
		// Lifting clears the record rather than leaving a stale author and
		// timestamp behind to be read as current.
		next.Reason = ""
	}
	settings["run_halt"] = next
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the halt")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditRunHaltChanged, "workspace", wsUUID,
		map[string]any{"halted": next.Halted, "reason": next.Reason}, nil)
	writeJSON(w, http.StatusOK, next)
}
