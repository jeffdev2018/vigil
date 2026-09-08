package handler

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fail-safe cancellation (JEF-275).
//
// A run that ends through /complete or /fail gets a terminal settlement: the
// writes it held for approval are dropped or offered, its replay chain is
// sealed into the audit log, and whatever barrier was waiting on it — fan-out,
// duel, eval case, campaign merge — is moved along.
//
// A CANCELLED run used to get none of that. Its held writes stayed pending
// forever, its replay was never sealed, and every barrier waiting on it waited
// for a run that would never report. Cancellation is terminal too, so it now
// runs the same settlement, minus the one piece its own callers already do
// (see cancelledRunBarriers).

// AuditRunCancelledCleanup records the settlement a cancelled run received.
const AuditRunCancelledCleanup = "run.cancelled_cleanup"

// terminalRunHooks is the settlement a run reported terminal by its daemon
// gets. succeeded distinguishes the completion (held writes become one
// decision) from a failure (held writes are dropped).
func (h *Handler) terminalRunHooks(ctx context.Context, task db.AgentTaskQueue, succeeded bool) {
	// Fan-out (K38): a settled child moves the barrier.
	h.updateFanoutBarrier(ctx, task)
	// Agent duel (K39): a finished candidate run moves the duel.
	h.updateDuelBarrier(ctx, task)
	// Eval Lab (K24): a finished replay is scored on the criteria it proved.
	h.settleEvalRunCase(ctx, task)
	// "Show me first" (K69): the held writes become one decision, or are dropped.
	h.settlePendingEffects(ctx, task, succeeded)
	// Replay (K70): seal the run's event chain into the audit log.
	h.sealRunReplay(ctx, task)
	// Refactoring campaigns (K42): a finished merge run moves the queue.
	h.updateCampaignMergeRun(ctx, task)
}

// cancelledRunBarriers moves the barriers that were waiting on a run the server
// stopped. Without this a cancelled child leaves its fan-out member, its duel
// or its campaign merge waiting for a report that will never come.
//
// settleEvalRunCase is deliberately NOT here. An eval replay is only cancelled
// by the paths that refuse to start it unconfined, and those settle the case
// themselves — as an infrastructure failure, which is not the same verdict as
// a scored zero. Re-settling it here would overwrite the more precise one.
func (h *Handler) cancelledRunBarriers(ctx context.Context, task db.AgentTaskQueue) {
	h.updateFanoutBarrier(ctx, task)
	h.updateDuelBarrier(ctx, task)
	h.updateCampaignMergeRun(ctx, task)
}

// afterTaskCancelled is TaskService.OnTaskCancelled: the terminal settlement
// for a run the server stopped. It runs after the cancelling transaction
// committed, once per cancelled task.
//
// A run cancelled before it ever started has no held writes and no event chain
// worth sealing, so it takes the barriers only — sealing every abandoned queued
// row would fill the audit log with empty replays.
func (h *Handler) afterTaskCancelled(ctx context.Context, task db.AgentTaskQueue) {
	h.cancelledRunBarriers(ctx, task)
	if !task.StartedAt.Valid {
		return
	}
	dropped := h.settlePendingEffects(ctx, task, false)
	h.sealRunReplay(ctx, task)
	wsIDStr := h.TaskService.ResolveTaskWorkspaceID(ctx, task)
	if wsIDStr == "" {
		return
	}
	h.audit(ctx, parseUUID(wsIDStr), "system", "", AuditRunCancelledCleanup, "task", task.ID,
		map[string]any{"dropped_effects": dropped, "failure_reason": task.FailureReason.String}, nil)
}
