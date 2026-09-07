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
	// Keep the invoking human's connected-app policy. Do not copy the service's
	// mutexes, event bus, analytics or wakeup hooks into an uncommitted enqueue.
	// The existing enqueue path requires a bus; this private bus has no
	// subscribers, so an uncommitted task cannot escape through listeners.
	scoped := &TaskService{Queries: q, Bus: events.New(), Composio: s.Composio, FeatureFlags: s.FeatureFlags}
	return scoped.enqueueMentionTaskWithCommentPlan(ctx, issue, source.AgentID, pgtype.UUID{}, nil,
		source.IsLeaderTask, source.SquadID, true, handoff, actorID, source.ID)
}
