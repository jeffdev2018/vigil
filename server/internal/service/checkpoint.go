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
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Checkpoints (K20). The resume point of a run is the last transcript seq
// reached at a safe boundary plus the runtime session the task already
// keeps. An infrastructure interruption (daemon offline, reconnect
// timeout, runtime recovery) retries from it through the existing retry
// chain, at most CheckpointResumeMaxAttempts times; beyond that the run
// fails with its own reason, distinct from an application failure.

const (
	CheckpointResumeMaxAttempts     = 3
	ReasonCheckpointResumeExhausted = "checkpoint_resume_exhausted"
)

var checkpointReasons = map[string]bool{
	string(taskfailure.ReasonRuntimeOffline):          true,
	string(taskfailure.ReasonRuntimeRecovery):         true,
	string(taskfailure.ReasonRuntimeReconnectTimeout): true,
}

// checkpointResume decides how an interrupted run continues: attempts is
// the child's counter when a resume is allowed; exhausted is true when the
// cap is reached and the run must fail distinctly instead.
func checkpointResume(parent db.AgentTaskQueue, reason string) (attempts pgtype.Int4, exhausted bool) {
	if !checkpointReasons[reason] {
		return pgtype.Int4{}, false
	}
	if parent.CheckpointAttempts >= CheckpointResumeMaxAttempts {
		return pgtype.Int4{}, true
	}
	return pgtype.Int4{Int32: parent.CheckpointAttempts + 1, Valid: true}, false
}

// recordCheckpointResume leaves the story on the interrupted run's
// transcript: when it broke, why, and which run continues from where.
func (s *TaskService) recordCheckpointResume(ctx context.Context, parent, child db.AgentTaskQueue, reason string) {
	seq, err := s.Queries.NextTaskMessageSeq(ctx, parent.ID)
	if err != nil {
		return
	}
	point := "the start"
	if parent.LastCheckpointSeq.Valid {
		point = fmt.Sprintf("checkpoint seq %d", parent.LastCheckpointSeq.Int64)
	}
	content := fmt.Sprintf("Run interrupted (%s) at %s. Resumed automatically as run %s from %s (attempt %d/%d).",
		reason, time.Now().UTC().Format(time.RFC3339), util.UUIDToString(child.ID), point, child.CheckpointAttempts, CheckpointResumeMaxAttempts)
	if _, err := s.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
		ID: dbid.NewV7(), TaskID: parent.ID, Seq: seq, Type: "system", Content: pgtype.Text{String: content, Valid: true},
	}); err != nil {
		slog.Warn("checkpoint: resume message failed", "task_id", util.UUIDToString(parent.ID), "error", err)
	}
}
