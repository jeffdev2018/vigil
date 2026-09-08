package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EnqueueIssueFollowupInTx reuses the normal rerun lineage and attribution
// without cancelling or merging any pending work. The caller verifies invoke
// permission, holds the issue lock, and links the human response in the same transaction.
// q must be transaction-bound. Publish and wake the daemon only after commit.
func (s *TaskService) EnqueueIssueFollowupInTx(ctx context.Context, q *db.Queries, issue db.Issue, source db.AgentTaskQueue, actorID pgtype.UUID, handoff string) (db.AgentTaskQueue, error) {
	if source.IssueID != issue.ID || source.ChatSessionID.Valid || (source.Status != "completed" && source.Status != "failed" && source.Status != "cancelled") || !actorID.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("follow-up requires a terminal issue run and a human actor")
	}
	// q is already the caller's open transaction (which holds the issue lock).
	// Do not copy TxStarter or Budget: createTaskWithBudget would Begin a second
	// connection and deadlock on LockIssueForDescriptionUpdate under concurrent
	// correction retries. A private bus has no subscribers, so an uncommitted
	// task cannot escape through listeners before the caller commits.
	scoped := &TaskService{Queries: q, Bus: events.New(), FeatureFlags: s.FeatureFlags}
	return scoped.enqueueMentionTaskWithCommentPlan(ctx, issue, source.AgentID, pgtype.UUID{}, nil,
		source.IsLeaderTask, source.SquadID, true, handoff, actorID, source.ID, RunGroupAttempt{})
}
