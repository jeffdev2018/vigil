package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// Halt freeze (JEF-257). Setting the workspace halt asks every running run
// to pause and stamps halt_frozen_at; lifting it resumes exactly the runs
// that marker names, as follow-up runs on the session each pause ack saved.

const haltLiftResumeNote = "The workspace halt that paused this run was lifted. Continue from where you stopped; nothing else changed."

// ResumeHaltFrozenTasks resumes every run the workspace halt froze and whose
// daemon acked the pause, clearing each marker as its resume child is
// enqueued. Runs a human paused by hand carry no marker (RequestTaskPause
// clears it) and stay paused. Returns how many runs were resumed.
func (s *TaskService) ResumeHaltFrozenTasks(ctx context.Context, workspaceID pgtype.UUID) int {
	paused, err := s.Queries.ListHaltFrozenPausedTasks(ctx, workspaceID)
	if err != nil {
		slog.Warn("halt lift: list frozen runs failed", "workspace_id", util.UUIDToString(workspaceID), "error", err)
		return 0
	}
	resumed := 0
	for _, t := range paused {
		child, ok := s.resumePausedOnOwnSession(ctx, t, haltLiftResumeNote)
		if !ok {
			continue
		}
		if err := s.Queries.ClearHaltFrozenMarker(ctx, t.ID); err != nil {
			slog.Warn("halt lift: clear freeze marker failed", "task_id", util.UUIDToString(t.ID), "error", err)
		}
		s.systemMessage(ctx, t.ID, fmt.Sprintf("Resumed at %s as run %s after the workspace halt was lifted.", time.Now().UTC().Format(time.RFC3339), util.UUIDToString(child.ID)))
		slog.Info("halt lift: resumed", "task_id", util.UUIDToString(t.ID), "resumed_by_task_id", util.UUIDToString(child.ID))
		resumed++
	}
	return resumed
}
