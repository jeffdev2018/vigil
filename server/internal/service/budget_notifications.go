package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func thresholdTicks(limit int64, basisPoints int32) int64 {
	quotient, remainder := limit/10_000, limit%10_000
	return quotient*int64(basisPoints) + (remainder*int64(basisPoints)+9_999)/10_000
}

// NotifyBudgetChange publishes the status invalidation and creates each
// threshold notice exactly once per policy period.
func (s *BudgetService) NotifyBudgetChange(ctx context.Context, workspaceID pgtype.UUID) {
	s.publishUpdated(workspaceID)
	policies, err := s.Queries.ListBudgetPolicies(ctx, workspaceID)
	if err != nil {
		return
	}
	for _, policy := range policies {
		s.notifyPolicyThreshold(ctx, policy)
	}
}

func (s *BudgetService) notifyPolicyThreshold(ctx context.Context, policy db.BudgetPolicy) {
	start, end, err := budgetPeriodBounds(policy.Period, s.now())
	if err != nil {
		return
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	period, err := q.GetBudgetPeriod(ctx, db.GetBudgetPeriodParams{
		PolicyID:    policy.ID,
		PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: end, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		return
	}
	total := period.SpentUsdTicks + period.ReservedUsdTicks
	kind := ""
	if total >= policy.LimitUsdTicks && !period.BlockNotifiedAt.Valid {
		kind = "budget_exceeded"
	} else if total >= thresholdTicks(policy.LimitUsdTicks, policy.WarnBps) && !period.WarnNotifiedAt.Valid {
		kind = "budget_warning"
	}
	if kind == "" {
		return
	}
	recipients, err := ListWorkspaceManagerNotificationRecipients(ctx, q, policy.WorkspaceID)
	if err != nil || len(recipients) == 0 {
		return
	}
	params := db.MarkBudgetWarnNotifiedParams{
		PolicyID: policy.ID, PeriodStart: period.PeriodStart, PeriodEnd: period.PeriodEnd,
	}
	if kind == "budget_exceeded" {
		_, err = q.MarkBudgetBlockNotified(ctx, db.MarkBudgetBlockNotifiedParams(params))
	} else {
		_, err = q.MarkBudgetWarnNotified(ctx, params)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		return
	}
	details, _ := json.Marshal(map[string]any{
		"policy_id": policy.ID.String(), "scope_type": policy.ScopeType,
		"scope_id": util.UUIDToString(policy.ScopeID), "spent_usd_ticks": period.SpentUsdTicks,
		"reserved_usd_ticks": period.ReservedUsdTicks, "limit_usd_ticks": policy.LimitUsdTicks,
		"period_end": end.Format(time.RFC3339),
	})
	title := "Budget threshold reached"
	body := fmt.Sprintf("This %s budget is at %d%% of its limit.", policy.ScopeType, policy.WarnBps/100)
	severity := "attention"
	if kind == "budget_exceeded" {
		title = "Budget limit reached"
		body = fmt.Sprintf("This %s budget has reached its limit. Enforced runs are paused until the period resets or an override is granted.", policy.ScopeType)
		severity = "action_required"
	}
	items := make([]db.InboxItem, 0, len(recipients))
	for _, recipient := range recipients {
		item, err := q.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: policy.WorkspaceID,
			RecipientType: recipient.Type, RecipientID: recipient.ID,
			Type: kind, Severity: severity, IssueID: pgtype.UUID{},
			Title: title, Body: pgtype.Text{String: body, Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			return
		}
		items = append(items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}
	for _, item := range items {
		s.publishBudgetInbox(item)
	}
}

func (s *BudgetService) publishBudgetInbox(item db.InboxItem) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type: protocol.EventInboxNew, WorkspaceID: item.WorkspaceID.String(),
		ActorType: "system",
		Payload: map[string]any{"item": map[string]any{
			"id": item.ID.String(), "workspace_id": item.WorkspaceID.String(),
			"recipient_type": item.RecipientType, "recipient_id": item.RecipientID.String(),
			"type": item.Type, "severity": item.Severity, "title": item.Title,
			"body": util.TextToPtr(item.Body), "read": item.Read, "archived": item.Archived,
			"created_at": util.TimestampToString(item.CreatedAt), "actor_type": util.TextToPtr(item.ActorType),
			"details": json.RawMessage(item.Details),
		}},
	})
}
