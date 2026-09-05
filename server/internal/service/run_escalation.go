package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Cascade escalation (JEF-272): when a completed run lands below the
// confidence threshold, the system first re-enqueues the same agent on a
// STRONGER runtime instead of going straight to human review. The cascade is
// bounded per issue by confidence_review.max_escalations; when the ceiling is
// reached or no stronger runtime exists, the JEF-240 human-review path runs.

// TaskEscalation is the record persisted under context.escalation of the
// escalated task. Attempt counts cascade hops: the original run carries no
// escalation (attempt 0), its first escalation carries attempt 1, and a run
// scored at attempt >= max_escalations goes straight to human review.
type TaskEscalation struct {
	FromTaskID    string `json:"from_task_id"`
	Reason        string `json:"reason"`
	Attempt       int    `json:"attempt"`
	FromRuntimeID string `json:"from_runtime_id,omitempty"`
}

// escalationReasonBelowThreshold is currently the only trigger for the
// cascade; the field exists so later triggers (e.g. failure-driven) can share
// the plumbing without a schema change.
const escalationReasonBelowThreshold = "below_threshold"

// taskEscalationAttempt reads escalation.attempt off a task's context JSONB,
// defaulting to 0 for tasks that were never escalated.
func taskEscalationAttempt(contextJSON []byte) int {
	if len(contextJSON) == 0 {
		return 0
	}
	var ctx struct {
		Escalation *TaskEscalation `json:"escalation"`
	}
	if err := json.Unmarshal(contextJSON, &ctx); err != nil || ctx.Escalation == nil {
		return 0
	}
	return ctx.Escalation.Attempt
}

// escalateRunToStrongerRuntime attempts one cascade hop for a below-threshold
// run. It reports whether the escalation was handled (a new task carries the
// retry, or the handoff merged into an already-pending one); false means the
// caller must fall back to the JEF-240 human-review path. Best-effort: like
// escalateRunToHumanReview, failures are logged, never propagated.
func (s *TaskService) escalateRunToStrongerRuntime(ctx context.Context, issue db.Issue, agent db.Agent, task db.AgentTaskQueue, conf TaskConfidence, cfg ConfidenceReview) bool {
	attempt := taskEscalationAttempt(task.Context)
	if attempt >= cfg.MaxEscalations {
		slog.Info("run confidence cascade: escalation ceiling reached, going to human review",
			"task_id", util.UUIDToString(task.ID), "attempt", attempt, "max_escalations", cfg.MaxEscalations)
		return false
	}

	target, ok := s.pickEscalationRuntime(ctx, agent, issue, task)
	if !ok {
		slog.Info("run confidence cascade: no stronger runtime available, going to human review",
			"task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(issue.ID))
		return false
	}

	escalation := TaskEscalation{
		FromTaskID:    util.UUIDToString(task.ID),
		Reason:        escalationReasonBelowThreshold,
		Attempt:       attempt + 1,
		FromRuntimeID: util.UUIDToString(task.RuntimeID),
	}
	escalationJSON, err := json.Marshal(escalation)
	if err != nil {
		slog.Warn("run confidence cascade: marshal escalation failed", "error", err)
		return false
	}

	note := fmt.Sprintf(
		"## Escalation (attempt %d)\n\n"+
			"The previous run completed below the confidence threshold: score **%.2f** (threshold %.2f).\n\n"+
			"Rationale: %s\n\n"+
			"This task was escalated to a stronger runtime (attempt %d). Please redo the work with the above in mind.",
		escalation.Attempt, conf.Score, conf.Threshold, conf.Rationale, escalation.Attempt)

	newTask, err := s.EnqueueTaskForIssueEscalation(ctx, issue, note, pgtype.UUID{}, target, escalationJSON)
	if err != nil {
		slog.Warn("run confidence cascade: escalation enqueue failed, going to human review",
			"task_id", util.UUIDToString(task.ID), "error", err)
		return false
	}
	slog.Info("run confidence cascade: escalated to a stronger runtime",
		"from_task_id", util.UUIDToString(task.ID),
		"task_id", util.UUIDToString(newTask.ID),
		"from_runtime_id", util.UUIDToString(task.RuntimeID),
		"to_runtime_id", util.UUIDToString(target),
		"attempt", escalation.Attempt)

	// A JEF-241 coalescing merge returns the ALREADY-pending task, which keeps
	// its own runtime: the note was delivered but no escalation happened, so
	// the task:escalated event would lie about to_runtime_id. The retry is
	// still in good hands (the pending run opens with the note), hence true.
	if newTask.RuntimeID != target {
		return true
	}
	s.publishTaskEscalated(agent.WorkspaceID, newTask, task, escalation)
	return true
}

// pickEscalationRuntime scores the JEF-237 routing candidates for the failed
// task's class and picks the hop target. A runtime already tried on this
// issue is never reconsidered, and the router's floor guard exclusions hold.
//
// "Stronger" criterion: when the failed runtime has a routing score for this
// class, only a candidate scoring STRICTLY better qualifies — escalating
// sideways would burn an attempt for nothing. When the failed runtime is
// unscored (cold class, missing usage rows), the best available candidate
// wins, scored pool first and never-sampled runtimes as a last resort: trying
// an unknown runtime still beats waking a human.
func (s *TaskService) pickEscalationRuntime(ctx context.Context, agent db.Agent, issue db.Issue, task db.AgentTaskQueue) (pgtype.UUID, bool) {
	runtimes, err := s.Queries.ListRoutingCandidateRuntimes(ctx, db.ListRoutingCandidateRuntimesParams{
		WorkspaceID:      agent.WorkspaceID,
		OwnerID:          agent.OwnerID,
		RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		slog.Warn("run confidence cascade: candidate query failed", "error", err)
		return pgtype.UUID{}, false
	}
	if len(runtimes) == 0 {
		return pgtype.UUID{}, false
	}
	stats, err := s.Queries.GetRoutingStats(ctx, db.GetRoutingStatsParams{
		WorkspaceID: agent.WorkspaceID,
		Since:       pgtype.Timestamptz{Time: time.Now().Add(-routingStatsWindow), Valid: true},
	})
	if err != nil {
		slog.Warn("run confidence cascade: stats query failed", "error", err)
		return pgtype.UUID{}, false
	}
	triedRows, err := s.Queries.ListDistinctTaskRuntimesForIssue(ctx, issue.ID)
	if err != nil {
		slog.Warn("run confidence cascade: tried-runtimes query failed", "error", err)
		return pgtype.UUID{}, false
	}
	tried := make(map[[16]byte]bool, len(triedRows))
	for _, id := range triedRows {
		tried[id.Bytes] = true
	}

	taskClass := task.TaskClass
	if taskClass == "" {
		taskClass = TaskClassGeneral
	}
	candidates := buildRoutingCandidates(agent, runtimes, stats, taskClass)
	scored := scoreRoutingCandidates(candidates)

	// The failed runtime's reference score is its best-scored (runtime, model)
	// pair — the run itself does not record which candidate the router picked.
	failedScore := 0.0
	failedScoreKnown := false
	for _, c := range scored {
		if c.runtimeID == task.RuntimeID && (!failedScoreKnown || c.score > failedScore) {
			failedScore, failedScoreKnown = c.score, true
		}
	}

	available := make([]*routingCandidate, 0, len(candidates))
	for _, c := range candidates {
		if tried[c.runtimeID.Bytes] || c.trace.ExcludedReason != "" {
			continue
		}
		available = append(available, c)
	}
	sort.SliceStable(available, func(i, j int) bool {
		a, b := available[i], available[j]
		if a.scored != b.scored {
			return a.scored // scored candidates before never-sampled ones
		}
		if a.scored && b.scored {
			return a.score > b.score
		}
		return false
	})

	for _, c := range available {
		if failedScoreKnown && (!c.scored || c.score <= failedScore) {
			continue
		}
		return c.runtimeID, true
	}
	return pgtype.UUID{}, false
}

// publishTaskEscalated broadcasts the cascade hop so live clients can show the
// retry landing on a stronger runtime without refetching the task list.
func (s *TaskService) publishTaskEscalated(workspaceID pgtype.UUID, newTask, fromTask db.AgentTaskQueue, escalation TaskEscalation) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskEscalated,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":         util.UUIDToString(newTask.ID),
			"from_task_id":    util.UUIDToString(fromTask.ID),
			"issue_id":        util.UUIDToString(newTask.IssueID),
			"from_runtime_id": escalation.FromRuntimeID,
			"to_runtime_id":   util.UUIDToString(newTask.RuntimeID),
			"attempt":         escalation.Attempt,
		},
	})
}
