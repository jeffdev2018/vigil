package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
	next, err := h.writeRunHalt(r.Context(), wsUUID, req.Halted, req.Reason, requestUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the halt")
		return
	}
	h.publish(protocol.EventRunHaltChanged, uuidToString(wsUUID), "member", requestUserID(r), map[string]any{"run_halt": next})
	writeJSON(w, http.StatusOK, next)
}

// writeRunHalt stores the halt (or its lifting) on the workspace and audits
// it. Shared by PUT /api/run-halt and the fleet kill switch.
func (h *Handler) writeRunHalt(ctx context.Context, wsUUID pgtype.UUID, halted bool, reason, userID string) (service.RunHalt, error) {
	ws, err := h.Queries.GetWorkspace(ctx, wsUUID)
	if err != nil {
		return service.RunHalt{}, err
	}
	settings := map[string]any{}
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &settings)
	}
	next := service.RunHalt{Halted: halted, Reason: reason}
	if next.Halted {
		next.HaltedBy = userID
		next.HaltedAt = time.Now().UTC().Format(time.RFC3339)
	} else {
		// Lifting clears the record rather than leaving a stale author and
		// timestamp behind to be read as current.
		next.Reason = ""
	}
	settings["run_halt"] = next
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(ctx, db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		return service.RunHalt{}, err
	}
	h.audit(ctx, wsUUID, "member", userID, AuditRunHaltChanged, "workspace", wsUUID, map[string]any{"halted": next.Halted, "reason": next.Reason}, nil)
	return next, nil
}
