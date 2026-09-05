package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F02: the agent_task_queue status matrix has no state engine — every
// transition is a WHERE guard on its UPDATE (documented at the head of
// queries/agent.sql). This table locks each guard: a refused transition is
// pgx.ErrNoRows and leaves the row untouched.

var allTaskStatuses = []string{
	"queued", "deferred", "dispatched", "waiting_local_directory",
	"running", "completed", "failed", "cancelled",
}

func TestTaskTransitionMatrix(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	q := testHandler.Queries
	text := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

	ops := []struct {
		name    string
		allowed map[string]bool
		run     func(id pgtype.UUID) error
	}{
		{
			name:    "start",
			allowed: set("dispatched", "waiting_local_directory"),
			run:     func(id pgtype.UUID) error { _, err := q.StartAgentTask(ctx, id); return err },
		},
		{
			name:    "wait_local_directory",
			allowed: set("dispatched"),
			run: func(id pgtype.UUID) error {
				_, err := q.MarkAgentTaskWaitingLocalDirectory(ctx, db.MarkAgentTaskWaitingLocalDirectoryParams{
					ID: id, WaitReason: text("/tmp/contested"), PrepareLeaseSecs: 60,
				})
				return err
			},
		},
		{
			name:    "complete",
			allowed: set("running"),
			run: func(id pgtype.UUID) error {
				_, err := q.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{ID: id, Result: []byte(`{}`)})
				return err
			},
		},
		{
			name:    "fail",
			allowed: set("dispatched", "running", "waiting_local_directory"),
			run: func(id pgtype.UUID) error {
				_, err := q.FailAgentTask(ctx, db.FailAgentTaskParams{ID: id, Error: text("boom")})
				return err
			},
		},
		{
			name:    "cancel",
			allowed: set("queued", "deferred", "dispatched", "waiting_local_directory", "running"),
			run:     func(id pgtype.UUID) error { _, err := q.CancelAgentTask(ctx, id); return err },
		},
	}

	for _, op := range ops {
		for _, status := range allTaskStatuses {
			t.Run(op.name+"/from_"+status, func(t *testing.T) {
				taskID := seedBatchTask(t, "matrix "+op.name+" "+status)
				dbfx.Exec(t, `UPDATE agent_task_queue SET status = $2 WHERE id = $1`, taskID, status)

				err := op.run(parseUUID(taskID))
				var after string
				dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&after)

				switch {
				case op.allowed[status] && err != nil:
					t.Fatalf("%s from %s: refused (%v), want allowed", op.name, status, err)
				case op.allowed[status] && after == status:
					t.Fatalf("%s from %s: status unchanged, want a transition", op.name, status)
				case !op.allowed[status] && !errors.Is(err, pgx.ErrNoRows):
					t.Fatalf("%s from %s: err = %v, want ErrNoRows (refused without write)", op.name, status, err)
				case !op.allowed[status] && after != status:
					t.Fatalf("%s from %s: status became %s, want untouched", op.name, status, after)
				}
			})
		}
	}
}

func set(statuses ...string) map[string]bool {
	m := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		m[s] = true
	}
	return m
}

// Silence the unused-import guard when the fixture helpers move.
var _ = testutil.Cols{}
