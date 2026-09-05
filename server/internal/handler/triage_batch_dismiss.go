package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// BatchDismissTriageItem is one dismissal outcome inside a batch.
type BatchDismissTriageItem struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"` // dismissed | not_found | not_pending | error
}

// BatchDismissTriageItems dismisses up to 100 items with one request and one
// shared reason. Like batch accept it always answers 200 with per-item
// outcomes, so a partially applied batch is reported rather than hidden.
func (h *Handler) BatchDismissTriageItems(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var req struct {
		ItemIDs []string `json:"item_ids"`
		Reason  string   `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.ItemIDs) == 0 {
		writeError(w, http.StatusBadRequest, "item_ids must not be empty")
		return
	}
	if len(req.ItemIDs) > triageMaxBatchAccept {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d items per batch", triageMaxBatchAccept))
		return
	}

	reason := pgtype.Text{String: req.Reason, Valid: req.Reason != ""}
	results := make([]BatchDismissTriageItem, 0, len(req.ItemIDs))
	for _, raw := range req.ItemIDs {
		itemID, err := util.ParseUUID(raw)
		if err != nil {
			results = append(results, BatchDismissTriageItem{ID: raw, Outcome: "not_found"})
			continue
		}
		entry := BatchDismissTriageItem{ID: util.UUIDToString(itemID)}
		item, err := h.Queries.DismissPendingTriageItem(r.Context(), db.DismissPendingTriageItemParams{
			ID:               itemID,
			WorkspaceID:      workspaceID,
			ResolutionReason: reason,
			ResolvedBy:       parseUUID(userID),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The row is missing, in another workspace, or already resolved —
			// all three are "nothing to dismiss here" from the caller's view.
			entry.Outcome = "not_pending"
		case err != nil:
			entry.Outcome = "error"
		default:
			entry.Outcome = "dismissed"
			h.publishTriageResolved(workspaceID, item.ID, triage.StateDismissed)
		}
		results = append(results, entry)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []BatchDismissTriageItem `json:"items"`
	}{Items: results})
}
