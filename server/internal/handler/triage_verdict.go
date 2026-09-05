package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TriageVerdict is an agent's suggestion on one pending item, stored in the
// item's verdict JSONB. It is advisory: the item stays pending and only a
// human accept/dismiss resolves it.
type TriageVerdict struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
}

// SetTriageVerdict records an agent's suggested verdict. This is the one
// triage write agents may make — every state transition stays human-only —
// so a human caller is refused here, the mirror image of RequireHumanActor.
func (h *Handler) SetTriageVerdict(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceIDRaw := h.resolveWorkspaceID(r)
	workspaceID, ok := parseUUIDOrBadRequest(w, workspaceIDRaw, "workspace_id")
	if !ok {
		return
	}
	itemID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceIDRaw)
	if actorType != "agent" {
		writeError(w, http.StatusForbidden, "only an agent may suggest a triage verdict")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, actorID, "agent_id")
	if !ok {
		return
	}

	var req TriageVerdict
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Verdict {
	case "accept", "dismiss":
	default:
		writeError(w, http.StatusBadRequest, "verdict must be one of: accept, dismiss")
		return
	}
	if len(req.Reason) > 2000 {
		writeError(w, http.StatusBadRequest, "reason must be at most 2000 characters")
		return
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the verdict")
		return
	}

	// "Show me first" (K69): a preview-mode run's verdict is held for approval.
	if _, taskID, preview := h.previewRun(r); preview {
		if eff, ok := h.recordPending(r, agentID, taskID, workspaceID, pgtype.UUID{}, service.EffectTriageVerdict, "triage_item", itemID,
			map[string]any{"verdict": req.Verdict, "reason": req.Reason}, map[string]any{"verdict": req.Verdict, "reason": req.Reason}, true); ok {
			writePending(w, eff, map[string]any{"item_id": util.UUIDToString(itemID), "verdict": req.Verdict, "verdict_reason": req.Reason, "pending_approval": true})
			return
		}
	}
	item, err := h.Queries.SetTriageItemVerdict(r.Context(), db.SetTriageItemVerdictParams{
		ID:             itemID,
		WorkspaceID:    workspaceID,
		Verdict:        encoded,
		VerdictAgentID: agentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "only a pending triage item can carry a verdict")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the verdict")
		return
	}
	// Undo (K69): reversal clears the suggestion.
	h.recordEffect(r, workspaceID, pgtype.UUID{}, service.EffectTriageVerdict, "triage_item", item.ID, map[string]any{}, map[string]any{"verdict": req.Verdict, "reason": req.Reason, "verdict_revision": item.VerdictRevision}, true)
	h.publishTriageUpdated(workspaceID, item.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"item_id":          util.UUIDToString(item.ID),
		"verdict":          req.Verdict,
		"verdict_reason":   req.Reason,
		"verdict_agent_id": util.UUIDToString(item.VerdictAgentID),
		"verdict_at":       item.VerdictAt.Time,
		"verdict_revision": item.VerdictRevision,
	})
}
