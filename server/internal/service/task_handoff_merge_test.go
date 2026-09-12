package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handoff coalescing (JEF-241). The pending slot is per (issue, agent,
// comment thread) and racing attempts are outside it, so an agent can hold
// several pending rows on one issue. A handoff enqueue carries no trigger
// comment: it can only have collided with the assignment-level row, and the
// note must land there — never on another thread's run, never on a run that
// was already dispatched with its prompt built.

func handoffIssue(t *testing.T, fx routingTestFixture, dbfx *testutil.Fixture) (db.Issue, *TaskService) {
	t.Helper()
	issueID := dbfx.Issue(t, "Handoff merge target", testutil.Cols{"assignee_type": "agent", "assignee_id": fx.agentID})
	svc := NewTaskService(db.New(fx.pool), fx.pool, nil, events.New())
	issue, err := svc.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	return issue, svc
}

func taskHandoffNote(t *testing.T, dbfx *testutil.Fixture, taskID string) string {
	t.Helper()
	var note pgtype.Text
	dbfx.QueryRow(t, `SELECT handoff_note FROM agent_task_queue WHERE id = $1`, taskID).Scan(&note)
	return note.String
}

func TestHandoffMergeTargetsTheAssignmentLevelPendingRow(t *testing.T) {
	fx, dbfx := fixedRoutingFixture(t)
	issue, svc := handoffIssue(t, fx, dbfx)
	threadComment := dbfx.Comment(t, util.UUIDToString(issue.ID), "please look at the flaky test")
	// The thread's run is inserted first so an unordered lookup meets it first.
	threadTask := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id": fx.runtimeA, "issue_id": util.UUIDToString(issue.ID), "trigger_comment_id": threadComment,
	})
	assignmentTask := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id": fx.runtimeA, "issue_id": util.UUIDToString(issue.ID),
	})

	merged, err := svc.EnqueueTaskForIssueWithHandoff(context.Background(), issue, "review rework: fix the lint errors", pgtype.UUID{})
	if err != nil {
		t.Fatalf("enqueue with handoff: %v", err)
	}
	if util.UUIDToString(merged.ID) != assignmentTask {
		t.Fatalf("note merged into %s, want the assignment-level pending row %s", util.UUIDToString(merged.ID), assignmentTask)
	}
	if note := taskHandoffNote(t, dbfx, threadTask); note != "" {
		t.Fatalf("thread run received the note %q", note)
	}
	if note := taskHandoffNote(t, dbfx, assignmentTask); note != "review rework: fix the lint errors" {
		t.Fatalf("assignment row note = %q", note)
	}
}

func TestHandoffMergeRefusesADispatchedRun(t *testing.T) {
	fx, dbfx := fixedRoutingFixture(t)
	issue, svc := handoffIssue(t, fx, dbfx)
	dispatched := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id": fx.runtimeA, "issue_id": util.UUIDToString(issue.ID),
		"status": "dispatched", "dispatched_at": testutil.Raw("now()"),
	})

	_, err := svc.EnqueueTaskForIssueWithHandoff(context.Background(), issue, "interview answer", pgtype.UUID{})
	if err == nil {
		t.Fatal("merging into a dispatched run reported success; its prompt is already built and the note is lost")
	}
	if !errors.Is(err, ErrDuplicatePendingTask) {
		t.Fatalf("err = %v, want it to wrap ErrDuplicatePendingTask", err)
	}
	if note := taskHandoffNote(t, dbfx, dispatched); note != "" {
		t.Fatalf("dispatched run was given the note %q", note)
	}
}
