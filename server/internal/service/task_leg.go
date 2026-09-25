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
// so it is not a sample of the worker's task class — a pr_walkthrough leg is
// review-like for the same reason. A retry, fallback,
// revision or escalation leg is — each is a real attempt at the same class.
// A benchmark leg (JEF-276) is a real attempt too: it is the same exam an
// eval replay runs, but pinned to one (runtime, model) candidate precisely so
// its outcome is evidence about that pair, so it DOES feed the statistics.
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
	LegRoleBenchmark  = "benchmark"
	LegRoleEscalation = "escalation"
	// LegRolePrWalkthrough (F05) narrates someone else's diff, so it is
	// review-like: it must not count as a sample of the worker's task class.
	LegRolePrWalkthrough = "pr_walkthrough"
	// LegRoleEpicStep (F18) writes one artifact of a project's epic pipeline
	// — a PRD, a tech plan, a wireframe, a ticket breakdown. Like a
	// walkthrough it produces a document about work rather than attempting
	// the work, so it is review-like and must not feed the routing statistics
	// as a sample of the host issue's task class.
	LegRoleEpicStep = "epic_step"
	// LegRoleSubagent (long tasks, brick 5) is an isolated in-process run a
	// native run delegated a bounded piece of work to; the parent verifies
	// its report against its tool journal. Its cost belongs to the parent's
	// workflow. Review-like for routing statistics: it did a piece of the
	// work, not the task the issue was routed for.
	LegRoleSubagent = "subagent"
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
