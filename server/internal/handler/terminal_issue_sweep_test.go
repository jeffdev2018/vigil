package handler

import (
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Two authorities can end an orphan and the daemon only ever had one: a dead
// process, recovered through the stale window. A live process working on an
// issue somebody closed looks healthy from every angle and keeps its slot, its
// budget and its tokens until it finishes on its own. This is the other one.
func TestSweepTasksOnTerminalIssues(t *testing.T) {
	ctx := t.Context()
	runtimeID := handlerTestRuntimeID(t)
	agentID := dbfx.Agent(t, "orphan agent "+uuid.NewString()[:6], runtimeID)

	task := func(t *testing.T, issueStatus, taskStatus string) (string, string) {
		t.Helper()
		issue := dbfx.Issue(t, "orphan issue "+uuid.NewString()[:6], testutil.Cols{"status": issueStatus})
		return issue, dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issue, "status": taskStatus})
	}
	statusOf := func(t *testing.T, id string) (string, string) {
		t.Helper()
		var status, reason string
		dbfx.QueryRow(t, `SELECT status, COALESCE(failure_reason, '') FROM agent_task_queue WHERE id = $1`, id).Scan(&status, &reason)
		return status, reason
	}

	_, done := task(t, "done", "running")
	_, cancelled := task(t, "cancelled", "queued")
	_, open := task(t, "in_progress", "running")

	if ended := testHandler.TaskService.SweepTasksOnTerminalIssues(ctx, 100); ended < 2 {
		t.Fatalf("ended %d runs, want at least the closed and the cancelled one", ended)
	}

	for _, id := range []string{done, cancelled} {
		status, reason := statusOf(t, id)
		if status != "failed" || reason != taskfailure.ReasonIssueTerminal.String() {
			t.Errorf("run on a terminal issue = %s / %s, want failed / %s", status, reason, taskfailure.ReasonIssueTerminal)
		}
	}
	if status, _ := statusOf(t, open); status != "running" {
		t.Errorf("a run on an open issue = %s, want it left alone", status)
	}

	// Idempotent: a second pass finds nothing left to end, because the runs it
	// ended are no longer queued or running.
	before := func() int {
		var n int
		dbfx.QueryRow(t, `SELECT COUNT(*) FROM agent_task_queue WHERE id = ANY($1)`, []string{done, cancelled}).Scan(&n)
		return n
	}()
	if before != 2 {
		t.Fatalf("both rows must survive: a sweep ends a run, it does not delete it")
	}
	testHandler.TaskService.SweepTasksOnTerminalIssues(ctx, 100)
	for _, id := range []string{done, cancelled} {
		if status, _ := statusOf(t, id); status != "failed" {
			t.Errorf("a second pass must change nothing, got %s", status)
		}
	}
}
