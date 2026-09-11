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
	child, ok := s.resumePausedOnOwnSession(ctx, t, preemptionResumeNoteLead)
	if !ok {
		return false
	}
	s.systemMessage(ctx, t.ID, fmt.Sprintf("Resumed at %s as run %s from its checkpoint.", time.Now().UTC().Format(time.RFC3339), util.UUIDToString(child.ID)))
	slog.Info("preemption: resumed", "task_id", util.UUIDToString(t.ID), "resumed_by_task_id", util.UUIDToString(child.ID))
	return true
}

// resumePausedOnOwnSession continues a paused run as a follow-up run pinned
// to the session the pause ack saved, then closes the paused row. Shared by
// the K41 preemption sweeper and the JEF-257 halt lift; the human resume
// endpoint (handler.ResumeRun) does the same three steps with its own HTTP
// error mapping.
// EnqueueResumeChild queues the follow-up run that continues a paused run.
// A resume continues the PAUSED TASK's session, so it is agent-explicit: the
// task's agent, not the issue's current assignee. Reassigning the issue
// mid-pause must not hand the saved session to another agent, and an
// unassigned issue must not make the run unresumable. The child is also
// pinned to the runtime that ran the paused task (runtime_pinned), because
// that is where the session's files live.
//
// A gone/archived agent or a runtime deleted mid-pause fails the enqueue:
// the caller keeps the pause marker so the run stays visibly paused for a
// human. A pending-slot collision merges the note into the waiting task,
// like every other handoff enqueue (JEF-241).
func (s *TaskService) EnqueueResumeChild(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, handoffNote string, actorUserID pgtype.UUID) (db.AgentTaskQueue, error) {
	attempt := RunGroupAttempt{}
	if task.RuntimeID.Valid {
		if _, err := s.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
			ID:          task.RuntimeID,
			WorkspaceID: issue.WorkspaceID,
		}); err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("resume runtime: %w", err)
		}
		attempt.RuntimeOverride = task.RuntimeID
	}
	child, err := s.enqueueMentionTaskWithCommentPlan(ctx, issue, task.AgentID, pgtype.UUID{}, nil, false, pgtype.UUID{}, false, handoffNote, actorUserID, pgtype.UUID{}, attempt)
	if err == nil || handoffNote == "" || !pendingSlotTakenErr(err) {
		return child, err
	}
	return s.mergeHandoffIntoPendingTaskForAgent(ctx, issue, task.AgentID, handoffNote)
}

func (s *TaskService) resumePausedOnOwnSession(ctx context.Context, t db.AgentTaskQueue, note string) (db.AgentTaskQueue, bool) {
	issue, err := s.Queries.GetIssue(ctx, t.IssueID)
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	child, err := s.EnqueueResumeChild(ctx, issue, t, note, pgtype.UUID{})
	if err != nil {
		slog.Warn("resume: enqueue failed", "task_id", util.UUIDToString(t.ID), "error", err)
		return db.AgentTaskQueue{}, false
	}
	if t.SessionID.Valid && t.SessionID.String != "" {
		_ = s.Queries.SetTaskResumeContext(ctx, db.SetTaskResumeContextParams{ID: child.ID, SessionID: t.SessionID, WorkDir: t.WorkDir})
	}
	if _, err := s.Queries.MarkTaskResumed(ctx, db.MarkTaskResumedParams{ID: t.ID, ResumedByTaskID: child.ID}); err != nil {
		slog.Warn("resume: mark resumed failed", "task_id", util.UUIDToString(t.ID), "error", err)
		return db.AgentTaskQueue{}, false
	}
	return child, true
}
