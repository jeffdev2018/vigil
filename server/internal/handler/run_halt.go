package handler

import (
	"context"
	"encoding/json"
	"log/slog"
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
// who held them and why, and how many in-flight runs the halt is currently
// freezing (JEF-257). Readable by every member: someone whose run was
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
	halt := service.RunHaltFromSettings(ws.Settings)
	if halt.Halted {
		if n, err := h.Queries.CountHaltFrozenTasks(r.Context(), wsUUID); err == nil {
			halt.FrozenCount = int(n)
		}
	}
	writeJSON(w, http.StatusOK, halt)
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
	h.publish(protocol.EventRunHaltChanged, uuidToString(wsUUID), "member", requestUserID(r), map[string]any{"run_halt": next, "frozen": next.FrozenCount, "resumed": next.ResumedCount})
	writeJSON(w, http.StatusOK, next)
}

// writeRunHalt stores the halt (or its lifting) on the workspace and audits
// it. Shared by PUT /api/run-halt and the fleet kill switch.
//
// JEF-257: setting the halt also freezes every in-flight run through the K19
// pause machinery (reversible — the halt buys time to look, it does not kill
// work), and lifting it resumes exactly the runs the halt froze. The freeze
// runs BEFORE the settings write so the claim gate closes last and the
// "halt on but runs live" window stays minimal; a freeze failure is logged
// and audited but never blocks the halt itself.
func (h *Handler) writeRunHalt(ctx context.Context, wsUUID pgtype.UUID, halted bool, reason, userID string) (service.RunHalt, error) {
	if _, err := h.Queries.GetWorkspace(ctx, wsUUID); err != nil {
		return service.RunHalt{}, err
	}
	frozen, resumed := 0, 0
	if halted {
		frozen = h.freezeWorkspaceRuns(ctx, wsUUID, userID)
	} else {
		resumed = h.liftWorkspaceHaltFreeze(ctx, wsUUID)
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
	// The counts are response data, not halt state: the persisted record keeps
	// them zero so a stored blob never reports a stale frozen number.
	// Merged server-side (MergeWorkspaceSettings): a read-modify-write of the
	// whole settings blob lost the writes of any concurrent settings PUT on
	// a different key.
	raw, _ := json.Marshal(map[string]any{"run_halt": next})
	if _, err := h.Queries.MergeWorkspaceSettings(ctx, db.MergeWorkspaceSettingsParams{ID: wsUUID, Settings: raw}); err != nil {
		return service.RunHalt{}, err
	}
	next.FrozenCount = frozen
	next.ResumedCount = resumed
	h.notifyRunHaltChanged(uuidToString(wsUUID))
	h.audit(ctx, wsUUID, "member", userID, AuditRunHaltChanged, "workspace", wsUUID, map[string]any{"halted": next.Halted, "reason": next.Reason, "frozen": frozen, "resumed": resumed}, nil)
	return next, nil
}

// freezeWorkspaceRuns asks every running run of the workspace to pause and
// stamps the halt marker, then revokes those runs' secrets at request time —
// the leak scenario cannot wait for the daemon's pause ack (the ack path
// revokes again and is idempotent). Returns how many runs were frozen.
func (h *Handler) freezeWorkspaceRuns(ctx context.Context, wsUUID pgtype.UUID, userID string) int {
	rows, err := h.Queries.RequestWorkspaceHaltFreeze(ctx, wsUUID)
	if err != nil {
		slog.Error("run halt: freeze in-flight runs failed; halt is still saved", "workspace_id", uuidToString(wsUUID), "error", err)
		h.audit(ctx, wsUUID, "member", userID, AuditRunHaltChanged, "workspace", wsUUID, map[string]any{"halted": true, "freeze_error": err.Error()}, nil)
		return 0
	}
	for _, row := range rows {
		h.revokeRunSecrets(ctx, row.ID, "run_halt_freeze", "system", "")
	}
	return len(rows)
}

// liftWorkspaceHaltFreeze releases the freeze: runs whose daemon never acked
// the pause are unfrozen outright (an offline daemon must not pause after
// the lift), and runs that did ack are resumed on their saved session.
// Returns how many runs were resumed.
func (h *Handler) liftWorkspaceHaltFreeze(ctx context.Context, wsUUID pgtype.UUID) int {
	if err := h.Queries.ClearUnackedHaltFreeze(ctx, wsUUID); err != nil {
		slog.Warn("run halt lift: clearing unacked freezes failed", "workspace_id", uuidToString(wsUUID), "error", err)
	}
	if h.TaskService == nil {
		return 0
	}
	return h.TaskService.ResumeHaltFrozenTasks(ctx, wsUUID)
}

// notifyRunHaltChanged nudges the workspace's daemons so every task watcher
// re-polls its control status immediately instead of on the 5s poll. Old
// daemons ignore the frame; the poll remains the fallback.
func (h *Handler) notifyRunHaltChanged(workspaceID string) {
	if h.DaemonRunHalt != nil {
		h.DaemonRunHalt.NotifyRunHaltChanged(workspaceID)
	}
}
