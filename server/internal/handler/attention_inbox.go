package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Attention Inbox (K02): a projection of inbox_item holding only what needs
// a human, ordered by risk instead of date. Nothing is stored: the score is
// computed on read because its inputs (age, urgency) move after the write.

type AttentionInboxItem struct {
	InboxItemResponse
	// RiskScore orders the list; higher first. Heuristic weights below.
	RiskScore int    `json:"risk_score"`
	Reason    string `json:"reason"`
}

// Fixed weights. ponytail: naive heuristic (severity + urgency + age); swap
// for a learned score once K25 scorecards exist.
const (
	attentionWeightActionRequired = 60
	attentionWeightAttention      = 30
	attentionWeightDecision       = 20
	attentionWeightUrgencyHigh    = 25
	attentionWeightUrgencyLow     = -10
	attentionWeightPerHour        = 2
	attentionMaxAgeBonus          = 48
)

func attentionScore(item db.ListInboxItemsRow, now time.Time) (int, string) {
	score := 0
	reason := item.Type
	switch item.Severity {
	case "action_required":
		score += attentionWeightActionRequired
	case "attention":
		score += attentionWeightAttention
	}
	if item.Type == "decision_request" || item.Type == "decision_escalated" {
		score += attentionWeightDecision
		var details struct {
			Urgency string `json:"urgency"`
		}
		if json.Unmarshal(item.Details, &details) == nil {
			switch details.Urgency {
			case "high":
				score += attentionWeightUrgencyHigh
				reason = "decision_urgent"
			case "low":
				score += attentionWeightUrgencyLow
			}
		}
		if item.Type == "decision_escalated" {
			// Someone already missed it: it outranks a fresh card, whatever
			// its urgency said.
			score += attentionWeightActionRequired
			reason = "decision_escalated"
		}
	}
	if item.CreatedAt.Valid {
		hours := int(now.Sub(item.CreatedAt.Time).Hours())
		if hours < 0 {
			hours = 0
		}
		if hours > attentionMaxAgeBonus {
			hours = attentionMaxAgeBonus
		}
		score += hours * attentionWeightPerHour
	}
	return score, reason
}

func (h *Handler) ListAttentionInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListAttentionInboxItems(r.Context(), db.ListAttentionInboxItemsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		slog.Warn("list attention inbox failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list attention inbox")
		return
	}
	now := time.Now()
	items := make([]AttentionInboxItem, 0, len(rows))
	for _, row := range rows {
		base := db.ListInboxItemsRow(row)
		score, reason := attentionScore(base, now)
		items = append(items, AttentionInboxItem{InboxItemResponse: inboxRowToResponse(base), RiskScore: score, Reason: reason})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].RiskScore > items[j].RiskScore })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// notifyDecisionRequested files one inbox item per workspace manager when a
// Decision Card is asked (K01 → K02). Best effort: the card exists either way.
func (h *Handler) notifyDecisionRequested(ctx context.Context, issue db.Issue, decision db.IssueDecision, actorType, actorID string) {
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, issue.WorkspaceID)
	if err != nil {
		slog.Warn("decision inbox: list recipients failed", "error", err, "issue_id", uuidToString(issue.ID))
		return
	}
	details, _ := json.Marshal(map[string]any{
		"decision_id": uuidToString(decision.ID),
		"urgency":     decision.Urgency,
		"question":    decision.Question,
	})
	severity := "attention"
	if decision.Urgency == "high" {
		severity = "action_required"
	}
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   issue.WorkspaceID,
			RecipientType: rcpt.Type,
			RecipientID:   rcpt.ID,
			Type:          "decision_request",
			Severity:      severity,
			IssueID:       issue.ID,
			Title:         issue.Title,
			Body:          pgtype.Text{String: decision.Question, Valid: true},
			ActorType:     pgtype.Text{String: actorType, Valid: true},
			ActorID:       parseUUID(actorID),
			Details:       details,
		})
		if err != nil {
			slog.Warn("decision inbox: create item failed", "error", err, "issue_id", uuidToString(issue.ID))
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
			"item": inboxToResponse(item),
		})
	}
}
