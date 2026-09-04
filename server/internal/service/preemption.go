package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Preemption (K41). P0 is the issue priority `urgent` (priority 4). When
// an urgent run is queued for an agent that has no free slot, the
// lowest-priority running run of that agent is asked to pause (K19: at its
// next safe boundary, never mid tool call). An urgent run is never
// preempted. Once capacity frees, the runtime sweeper resumes preempted
// runs from their checkpoint, priority first then age, as follow-up runs
// on the same session.

const (
	PriorityUrgent           = int32(4)
	preemptionResumeNoteLead = "Resumed automatically after being suspended to let an urgent issue go first. Continue from where you stopped; nothing else changed."
)

// PreemptForUrgentTask asks one run to pause when task is urgent and the
// agent is saturated. Returns the preempted run when one was chosen.
func (s *TaskService) PreemptForUrgentTask(ctx context.Context, task db.AgentTaskQueue, agent db.Agent) *db.AgentTaskQueue {
	if task.Priority < PriorityUrgent || !task.IssueID.Valid {
		return nil
	}
	running, err := s.Queries.CountRunningTasks(ctx, agent.ID)
	if err != nil || running < int64(agent.MaxConcurrentTasks) {
		return nil
	}
	candidates, err := s.Queries.ListRunningTasksForAgentByPriority(ctx, agent.ID)
	if err != nil || len(candidates) == 0 {
		return nil
	}
	victim := candidates[0]
	if victim.Priority >= PriorityUrgent {
		return nil // an urgent run keeps its slot
	}
	preempted, err := s.Queries.MarkTaskPreempted(ctx, db.MarkTaskPreemptedParams{ID: victim.ID, PreemptedByTaskID: task.ID})
	if err != nil {
		slog.Warn("preemption: mark failed", "task_id", util.UUIDToString(victim.ID), "error", err)
		return nil
	}
	s.systemMessage(ctx, preempted.ID, fmt.Sprintf("Suspended at %s to let an urgent issue go first (run %s). This run resumes from its checkpoint once capacity frees.", time.Now().UTC().Format(time.RFC3339), util.UUIDToString(task.ID)))
	slog.Info("preemption: pause requested", "task_id", util.UUIDToString(victim.ID), "for_task_id", util.UUIDToString(task.ID))
	return &preempted
}

func (s *TaskService) systemMessage(ctx context.Context, taskID pgtype.UUID, content string) {
	seq, err := s.Queries.NextTaskMessageSeq(ctx, taskID)
	if err != nil {
		return
	}
	_, _ = s.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{ID: dbid.NewV7(), TaskID: taskID, Seq: seq, Type: "system", Content: pgtype.Text{String: content, Valid: true}})
}

// ResumePreemptedTasks is the runtime sweeper stage: preempted runs whose
// agent has a free slot and no urgent run waiting continue as follow-up
// runs on their own session, priority first then age. Returns how many.
func (s *TaskService) ResumePreemptedTasks(ctx context.Context, maxPerTick int32) int {
	paused, err := s.Queries.ListPreemptedPausedTasks(ctx, maxPerTick)
	if err != nil {
		return 0
	}
	resumed := 0
	for _, t := range paused {
		agent, err := s.Queries.GetAgent(ctx, t.AgentID)
		if err != nil {
			continue
		}
		busy, err := s.Queries.CountCapacityBearingTasks(ctx, agent.ID)
		if err != nil || busy >= int64(agent.MaxConcurrentTasks) {
			continue
		}
		if urgent, err := s.Queries.CountQueuedUrgentTasksForAgent(ctx, agent.ID); err != nil || urgent > 0 {
			continue // an urgent run waiting takes the slot first
		}
		if s.resumePreempted(ctx, t) {
			resumed++
		}
	}
	return resumed
}

func (s *TaskService) resumePreempted(ctx context.Context, t db.AgentTaskQueue) bool {
	issue, err := s.Queries.GetIssue(ctx, t.IssueID)
	if err != nil {
		return false
	}
	child, err := s.EnqueueTaskForIssueWithHandoff(ctx, issue, preemptionResumeNoteLead, pgtype.UUID{})
	if err != nil {
		slog.Warn("preemption: resume enqueue failed", "task_id", util.UUIDToString(t.ID), "error", err)
		return false
	}
	if t.SessionID.Valid && t.SessionID.String != "" {
		_ = s.Queries.SetTaskResumeContext(ctx, db.SetTaskResumeContextParams{ID: child.ID, SessionID: t.SessionID, WorkDir: t.WorkDir})
	}
	if _, err := s.Queries.MarkTaskResumed(ctx, db.MarkTaskResumedParams{ID: t.ID, ResumedByTaskID: child.ID}); err != nil {
		slog.Warn("preemption: mark resumed failed", "task_id", util.UUIDToString(t.ID), "error", err)
		return false
	}
	s.systemMessage(ctx, t.ID, fmt.Sprintf("Resumed at %s as run %s from its checkpoint.", time.Now().UTC().Format(time.RFC3339), util.UUIDToString(child.ID)))
	slog.Info("preemption: resumed", "task_id", util.UUIDToString(t.ID), "resumed_by_task_id", util.UUIDToString(child.ID))
	return true
}
