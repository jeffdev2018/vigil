package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Triage auto-ML (K61): every accept or dismiss a human makes is an example.
// A pending item is compared to its ten nearest resolved neighbours (full
// text on title, body and payload); the weighted majority is the suggestion
// and its share of the weight is the confidence. Above the workspace
// threshold, and only when the workspace turned it on, the queue applies
// the suggestion itself, reversibly, and says so.

const (
	AuditTriageAutoDecided = "triage.auto_decided"
	triageMaxSuggestionIDs = 50
)

type TriageNeighbor struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	State string  `json:"state"`
	Score float64 `json:"score"`
}

type TriageSuggestion struct {
	ItemID      string           `json:"item_id"`
	Ready       bool             `json:"ready"`
	Examples    int64            `json:"examples"`
	MinExamples int              `json:"min_examples"`
	Suggested   string           `json:"suggested,omitempty"` // accept | dismiss
	Confidence  float64          `json:"confidence"`
	Neighbors   []TriageNeighbor `json:"neighbors"`
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}]{3,}`)

// triageQueryFor turns an item's title into an any-word query.
func triageQueryFor(item db.TriageItem) string {
	seen := map[string]bool{}
	var words []string
	for _, w := range wordRe.FindAllString(strings.ToLower(item.Title), -1) {
		if !seen[w] {
			seen[w] = true
			words = append(words, w)
		}
		if len(words) >= 12 {
			break
		}
	}
	return strings.Join(words, " or ")
}

func (h *Handler) triageSuggestionFor(ctx context.Context, item db.TriageItem, cfg service.TriageAuto) (TriageSuggestion, error) {
	examples, err := h.Queries.CountTriageExamples(ctx, item.WorkspaceID)
	if err != nil {
		return TriageSuggestion{}, err
	}
	s := TriageSuggestion{ItemID: uuidToString(item.ID), Examples: examples, MinExamples: cfg.MinExamples, Ready: examples >= int64(cfg.MinExamples), Neighbors: []TriageNeighbor{}}
	q := triageQueryFor(item)
	if q == "" {
		return s, nil
	}
	rows, err := h.Queries.ListTriageNeighbors(ctx, db.ListTriageNeighborsParams{WorkspaceID: item.WorkspaceID, Query: q, ExcludeID: item.ID})
	if err != nil {
		return TriageSuggestion{}, err
	}
	weights := map[string]float64{}
	total := 0.0
	for _, r := range rows {
		s.Neighbors = append(s.Neighbors, TriageNeighbor{ID: uuidToString(r.ID), Title: r.Title, State: r.State, Score: r.Score})
		weights[r.State] += r.Score
		total += r.Score
	}
	if total == 0 {
		return s, nil
	}
	best, bestW := "", 0.0
	for state, w := range weights {
		if w > bestW {
			best, bestW = state, w
		}
	}
	if best == "accepted" {
		s.Suggested = "accept"
	} else {
		s.Suggested = "dismiss"
	}
	s.Confidence = bestW / total
	return s, nil
}

// GET /api/triage/suggestions?ids=a,b,c — suggestions for visible items.
func (h *Handler) GetTriageSuggestions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	var ids []pgtype.UUID
	for _, raw := range strings.Split(r.URL.Query().Get("ids"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var id pgtype.UUID
		if err := id.Scan(raw); err != nil {
			writeError(w, http.StatusBadRequest, "ids must be UUIDs")
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > triageMaxSuggestionIDs {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("ids must hold 1 to %d items", triageMaxSuggestionIDs))
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.TriageAutoSettings(ws.Settings)
	items, err := h.Queries.ListTriageItemsByIDs(r.Context(), db.ListTriageItemsByIDsParams{WorkspaceID: wsUUID, Ids: ids})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load items")
		return
	}
	out := map[string]TriageSuggestion{}
	for _, item := range items {
		s, err := h.triageSuggestionFor(r.Context(), item, cfg)
		if err != nil {
			slog.Warn("triage suggestion failed", append(logger.RequestAttrs(r), "error", err, "item_id", uuidToString(item.ID))...)
			continue
		}
		out[uuidToString(item.ID)] = s
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out, "auto": cfg})
}

// POST /api/triage/items/{id}/reopen — a dismissed item (by a human, a rule
// or the auto-classifier) goes back to pending.
func (h *Handler) ReopenTriageItem(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "item id")
	if !ok {
		return
	}
	item, err := h.Queries.ReopenDismissedTriageItem(r.Context(), db.ReopenDismissedTriageItemParams{ID: id, WorkspaceID: wsUUID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusConflict, "triage_not_dismissed", "only a dismissed item can be reopened")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reopen the item")
		return
	}
	h.publishTriageResolved(wsUUID, item.ID, "pending")
	writeJSON(w, http.StatusOK, map[string]any{"id": uuidToString(item.ID), "state": item.State})
}

// autoTriage applies a confident suggestion when the workspace allows it.
// Called after the rules (K62) let an item through.
func (h *Handler) autoTriage(ctx context.Context, item db.TriageItem) {
	ws, err := h.Queries.GetWorkspace(ctx, item.WorkspaceID)
	if err != nil {
		return
	}
	cfg := service.TriageAutoSettings(ws.Settings)
	if !cfg.Enabled {
		return
	}
	s, err := h.triageSuggestionFor(ctx, item, cfg)
	if err != nil || !s.Ready || s.Suggested == "" || s.Confidence < cfg.Threshold {
		return
	}
	reason := fmt.Sprintf("auto: %.0f%% confidence from %d similar deliveries", s.Confidence*100, len(s.Neighbors))
	switch s.Suggested {
	case "dismiss":
		if _, err := h.Queries.AutoDismissPendingTriageItem(ctx, db.AutoDismissPendingTriageItemParams{ID: item.ID, WorkspaceID: item.WorkspaceID, ResolutionReason: pgtype.Text{String: reason, Valid: true}}); err != nil {
			slog.Warn("triage auto: dismiss failed", "error", err, "item_id", uuidToString(item.ID))
			return
		}
		h.publishTriageResolved(item.WorkspaceID, item.ID, "dismissed")
	case "accept":
		leads, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, item.WorkspaceID)
		if err != nil || len(leads) == 0 {
			return
		}
		actor := ""
		for _, l := range leads {
			if l.Type == "member" {
				actor = uuidToString(l.ID)
				break
			}
		}
		if actor == "" {
			return
		}
		res := h.acceptTriageItemCore(ctx, item.WorkspaceID, actor, item.ID, triageAcceptOverrides{})
		if res.outcome != "accepted" {
			return
		}
		h.publishTriageResolved(item.WorkspaceID, item.ID, "accepted")
	}
	h.audit(ctx, item.WorkspaceID, "system", "", AuditTriageAutoDecided, "triage_item", item.ID, map[string]any{"decision": s.Suggested, "confidence": s.Confidence, "neighbors": s.Neighbors, "examples": s.Examples}, nil)
	h.publish(protocol.EventTriageNew, uuidToString(item.WorkspaceID), "system", "", map[string]any{"item_id": uuidToString(item.ID), "auto": s.Suggested})
}
