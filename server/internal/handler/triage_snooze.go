package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// triageMaxSnooze bounds a snooze: past it, an item that nobody came back to
// is a retention problem, not a scheduling one.
const triageMaxSnooze = 30 * 24 * time.Hour

// SnoozeTriageItem parks one pending item until a chosen time. The item stays
// pending — a snooze hides work, it never resolves it — and drops out of the
// default queue listing until `until` passes.
func (h *Handler) SnoozeTriageItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	itemID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req struct {
		Until string `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	until, err := time.Parse(time.RFC3339, req.Until)
	if err != nil {
		writeError(w, http.StatusBadRequest, "until must be an RFC3339 timestamp")
		return
	}
	now := time.Now()
	if !until.After(now) {
		writeError(w, http.StatusBadRequest, "until must be in the future")
		return
	}
	if until.After(now.Add(triageMaxSnooze)) {
		writeError(w, http.StatusBadRequest, "until must be at most 30 days from now")
		return
	}

	item, err := h.Queries.SnoozePendingTriageItem(r.Context(), db.SnoozePendingTriageItemParams{
		ID:           itemID,
		WorkspaceID:  workspaceID,
		SnoozedUntil: pgtype.Timestamptz{Time: until.UTC(), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the item is gone or it is no longer pending; both mean the
		// caller's view of the queue is stale.
		writeError(w, http.StatusConflict, "only a pending triage item can be snoozed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snooze triage item")
		return
	}
	h.publishTriageUpdated(workspaceID, item.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"item_id":       util.UUIDToString(item.ID),
		"state":         item.State,
		"snoozed_until": item.SnoozedUntil.Time,
	})
}

// WakeDueSnoozedTriageItems clears snoozes whose time has come and
// re-announces each item. The listing already stopped hiding a due item, so
// this sweep only owns the live re-announcement.
func (h *Handler) WakeDueSnoozedTriageItems(ctx context.Context) (int64, error) {
	rows, err := h.Queries.WakeDueSnoozedTriageItems(ctx, triageRetentionSweepBatch)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		h.Bus.Publish(events.Event{
			Type:        protocol.EventTriageNew,
			WorkspaceID: util.UUIDToString(row.WorkspaceID),
			ActorType:   "system",
			Payload:     map[string]any{"item_id": util.UUIDToString(row.ID)},
		})
	}
	return int64(len(rows)), nil
}

func (h *Handler) publishTriageUpdated(workspaceID, itemID pgtype.UUID) {
	h.Bus.Publish(events.Event{
		Type:        protocol.EventTriageUpdated,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		Payload:     map[string]any{"item_id": util.UUIDToString(itemID)},
	})
}

// SweepTriageQueue is the scheduler's triage tick: items nobody resolved
// inside their retention window leave the queue as `expired`, and snoozes
// whose time has come are cleared and re-announced.
func (h *Handler) SweepTriageQueue(ctx context.Context) (int64, error) {
	expired, err := h.ExpireStaleTriageItems(ctx)
	if err != nil {
		return expired, err
	}
	woken, err := h.WakeDueSnoozedTriageItems(ctx)
	return expired + woken, err
}
