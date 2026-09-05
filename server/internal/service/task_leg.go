package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Leg roles (JEF-274). A run's leg_role says what it is inside its workflow;
// the empty string is the primary (draft/single) leg every other leg points
// back at.
//
// The split that matters beyond display is in GetRoutingStats: a review-like
// leg (review, critique, answer, watchdog, eval) judges someone else's work,
// so it is not a sample of the worker's task class. A retry, fallback,
// revision or escalation leg is — each is a real attempt at the same class.
const (
	LegRoleRetry      = "retry"
	LegRoleFallback   = "fallback"
	LegRoleRerun      = "rerun"
	LegRoleReview     = "review"
	LegRoleCritique   = "critique"
	LegRoleAnswer     = "answer"
	LegRoleRevision   = "revision"
	LegRoleWatchdog   = "watchdog"
	LegRoleDuel       = "duel"
	LegRoleFanout     = "fanout"
	LegRoleShard      = "shard"
	LegRoleEval       = "eval"
	LegRoleEscalation = "escalation"
)

// WorkflowRoot is the run every leg of parent's workflow points at: parent's
// own root when it already belongs to one, else parent itself. A zero-value
// parent yields an invalid UUID, which stamps NULL — the leg is its own root.
func WorkflowRoot(parent db.AgentTaskQueue) pgtype.UUID {
	if parent.WorkflowRootTaskID.Valid {
		return parent.WorkflowRootTaskID
	}
	return parent.ID
}

// RetryLegRole tells a plain retry from a fallback: a retry that carries
// failover history was moved to another runtime (K28) rather than re-run
// where it failed.
func RetryLegRole(failoverHistory []byte) string {
	if len(decodeFailoverHistory(failoverHistory)) > 0 {
		return LegRoleFallback
	}
	return LegRoleRetry
}

// StampLeg records a freshly created run's role and links it to the workflow
// root. Pass a zero-value parent for a producer with no originating run.
func (s *TaskService) StampLeg(ctx context.Context, task db.AgentTaskQueue, role string, parent db.AgentTaskQueue) (db.AgentTaskQueue, error) {
	return stampLeg(ctx, s.Queries, task, role, parent)
}

// stampLeg is the transaction-aware form: the retry producers create their
// child inside the same transaction that fails the parent, so the stamp has
// to commit with it or the leg would be lost on rollback.
func stampLeg(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, role string, parent db.AgentTaskQueue) (db.AgentTaskQueue, error) {
	return q.SetTaskLeg(ctx, db.SetTaskLegParams{
		ID:                 task.ID,
		LegRole:            role,
		WorkflowRootTaskID: WorkflowRoot(parent),
	})
}
