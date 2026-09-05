package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Decision SLA (K35): cards asked under a policy carry a deadline. The
// scheduler calls EscalateOverdueDecisions every few minutes; an overdue
// card steps to level 1 (the substitute hears about it) and, one deadline
// later, to level 2 (the workspace leads). Answering stops it: only
// unanswered cards are ever listed.

const (
	DecisionEscalationSubstitute = 1
	DecisionEscalationLeads      = 2
)

// decisionDeadline is the deadline a card asked now gets, or null without a
// policy.
func (h *Handler) decisionDeadline(ctx context.Context, wsID pgtype.UUID) pgtype.Timestamptz {
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	sla, ok := service.DecisionSLASettings(ws.Settings)
	if !ok {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: time.Now().Add(time.Duration(sla.DeadlineMinutes) * time.Minute), Valid: true}
}

// EscalateOverdueDecisions steps every overdue card once and notifies the
// level's recipients. It returns how many cards moved.
func (h *Handler) EscalateOverdueDecisions(ctx context.Context, now time.Time) (int, error) {
	overdue, err := h.Queries.ListOverdueIssueDecisions(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("list overdue decisions: %w", err)
	}
	moved := 0
	for _, d := range overdue {
		ws, err := h.Queries.GetWorkspace(ctx, d.WorkspaceID)
		if err != nil {
			continue
		}
		sla, ok := service.DecisionSLASettings(ws.Settings)
		if !ok {
			// The policy went away: the card keeps waiting, quietly.
			continue
		}
		level := d.EscalationLevel + 1
		next := pgtype.Timestamptz{Time: now.Add(time.Duration(sla.DeadlineMinutes) * time.Minute), Valid: true}
		if level >= DecisionEscalationLeads {
			next = pgtype.Timestamptz{}
		}
		escalated, err := h.Queries.EscalateIssueDecision(ctx, db.EscalateIssueDecisionParams{
			ID: d.ID, EscalationLevel: level, EscalatedAt: pgtype.Timestamptz{Time: now, Valid: true}, SlaDeadlineAt: next,
		})
		if err != nil {
			// Answered or stepped by a concurrent tick meanwhile.
			continue
		}
		moved++
		h.notifyDecisionEscalated(ctx, escalated, sla)
		h.audit(ctx, escalated.WorkspaceID, "system", "", AuditDecisionEscalated, "issue_decision", escalated.ID, map[string]any{"issue_id": uuidToString(escalated.IssueID), "level": escalated.EscalationLevel}, nil)
	}
	return moved, nil
}

// escalationRecipients: the substitute at level 1 when the policy names one
// who is still a member, otherwise (and always at level 2) the leads.
func (h *Handler) escalationRecipients(ctx context.Context, d db.IssueDecision, sla service.DecisionSLA) []service.AutopilotNotificationRecipient {
	if d.EscalationLevel == DecisionEscalationSubstitute && sla.SubstituteUserID != "" {
		if id, err := util.ParseUUID(sla.SubstituteUserID); err == nil {
			if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: id, WorkspaceID: d.WorkspaceID}); err == nil {
				return []service.AutopilotNotificationRecipient{{Type: "member", ID: id}}
			}
		}
	}
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, d.WorkspaceID)
	if err != nil {
		slog.Warn("decision sla: list leads failed", "error", err, "decision_id", uuidToString(d.ID))
		return nil
	}
	return recipients
}

func (h *Handler) notifyDecisionEscalated(ctx context.Context, d db.IssueDecision, sla service.DecisionSLA) {
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: d.IssueID, WorkspaceID: d.WorkspaceID})
	if err != nil {
		return
	}
	details, _ := json.Marshal(map[string]any{
		"decision_id":      uuidToString(d.ID),
		"urgency":          d.Urgency,
		"question":         d.Question,
		"escalation_level": d.EscalationLevel,
	})
	wsID := uuidToString(d.WorkspaceID)
	for _, rcpt := range h.escalationRecipients(ctx, d, sla) {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   d.WorkspaceID,
			RecipientType: rcpt.Type,
			RecipientID:   rcpt.ID,
			Type:          "decision_escalated",
			Severity:      "action_required",
			IssueID:       issue.ID,
			Title:         issue.Title,
			Body:          pgtype.Text{String: d.Question, Valid: true},
			ActorType:     pgtype.Text{String: d.AskedByType, Valid: true},
			ActorID:       d.AskedByID,
			Details:       details,
		})
		if err != nil {
			slog.Warn("decision sla: create inbox item failed", "error", err, "decision_id", uuidToString(d.ID))
			continue
		}
		h.publish(protocol.EventInboxNew, wsID, d.AskedByType, uuidToString(d.AskedByID), map[string]any{"item": inboxToResponse(item)})
	}
	h.publish(protocol.EventIssueUpdated, wsID, d.AskedByType, uuidToString(d.AskedByID), map[string]any{
		"issue": issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID)), "plan_changed": true,
	})
}
