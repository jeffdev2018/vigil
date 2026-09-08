package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Review gate and rework loop (JEF-238): with the project's gate on, done
// needs an approving latest review; a request_changes verdict re-queues the
// worker with the report as its brief until max_cycles, then escalates.

// seedReviewReport writes the completed worker run, its completed review run
// and the reviewer's report message that the gate reads back.
func seedReviewReport(t *testing.T, issue, worker, workerRuntime, verdict string) (workerTask, reviewTask string) {
	t.Helper()
	workerTask = dbfx.Task(t, worker, testutil.Cols{"runtime_id": workerRuntime, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()")})
	reviewTask = dbfx.Task(t, worker, testutil.Cols{"runtime_id": workerRuntime, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()"), "review_of_task_id": workerTask})
	dbfx.Insert(t, "task_message", testutil.Cols{
		"task_id": reviewTask, "seq": 1, "type": "review_report",
		"content": fmt.Sprintf(`{"verdict":%q,"risks":[],"questions":[],"suggestions":[]}`, verdict),
	})
	return workerTask, reviewTask
}

func gateMessage(resp *testutil.Response) string {
	msg, _ := resp.Map()["error"].(string)
	return msg
}

func TestReviewGateOnDone(t *testing.T) {
	project := dbfx.Project(t, "gated project")
	reviewConfigCleanup(t, project)
	runtimeID := handlerTestRuntimeID(t)
	worker := dbfx.Agent(t, "gated worker", runtimeID)

	// Gate off (no row): done goes through even without any review.
	ungated := dbfx.Issue(t, "ungated issue", testutil.Cols{"status": "in_review", "project_id": project})
	setIssueStatus(t, ungated, "done").Want(http.StatusOK)

	putReviewConfig(t, project, map[string]any{"gate_enabled": true, "max_cycles": 3}).Want(http.StatusOK)

	// No review yet: 409 "awaiting review", and the status stays put.
	issue := dbfx.Issue(t, "gated issue", testutil.Cols{"status": "in_review", "project_id": project})
	resp := setIssueStatus(t, issue, "done").Want(http.StatusConflict)
	if msg := gateMessage(resp); msg != "review gate: awaiting review" {
		t.Fatalf("message = %q", msg)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&status)
	if status != "in_review" {
		t.Fatalf("status after refused move = %q", status)
	}
	// A non-done move is not gated.
	setIssueStatus(t, issue, "in_progress").Want(http.StatusOK)

	// A request_changes verdict keeps the issue out of done.
	seedReviewReport(t, issue, worker, runtimeID, "request_changes")
	resp = setIssueStatus(t, issue, "done").Want(http.StatusConflict)
	if msg := gateMessage(resp); msg != "review gate: latest review verdict is request_changes" {
		t.Fatalf("message = %q", msg)
	}
	// The batch path is gated the same way.
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issue},
		"updates":   map[string]any{"status": "done"},
	})).Want(http.StatusConflict)

	// The latest review approving lifts the gate.
	workerTask, _ := seedReviewReport(t, issue, worker, runtimeID, "approve")
	_ = workerTask
	setIssueStatus(t, issue, "done").Want(http.StatusOK)
}

// storeReviewOutput runs the report-storage path the daemon completion calls.
func storeReviewOutput(t *testing.T, reviewTaskID, output string) {
	t.Helper()
	testHandler.storeCrossReviewReport(context.Background(), mustTask(t, reviewTaskID), output)
}

func TestReviewGateReworkLoop(t *testing.T) {
	ctx := context.Background()
	project := dbfx.Project(t, "rework project")
	reviewConfigCleanup(t, project)
	runtimeID := handlerTestRuntimeID(t)
	worker := dbfx.Agent(t, "rework worker", runtimeID)
	putReviewConfig(t, project, map[string]any{"gate_enabled": true, "max_cycles": 2}).Want(http.StatusOK)
	issue := dbfx.Issue(t, "rework issue", testutil.Cols{"status": "in_progress", "project_id": project, "assignee_type": "agent", "assignee_id": worker})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE issue_id = $1)`, issue)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue)
	})

	var mu sync.Mutex
	var reworks, escalations []map[string]any
	collect := func(list *[]map[string]any) func(events.Event) {
		return func(e events.Event) {
			payload, ok := e.Payload.(map[string]any)
			if !ok || payload["issue_id"] != issue {
				return
			}
			mu.Lock()
			*list = append(*list, payload)
			mu.Unlock()
		}
	}
	testHandler.Bus.Subscribe("cross_review:rework", collect(&reworks))
	testHandler.Bus.Subscribe("cross_review:escalated", collect(&escalations))

	countQueuedWorkerRuns := func() int {
		return dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND review_of_task_id IS NULL AND status = 'queued'`, issue)
	}

	// Cycle 1: request_changes re-queues the worker with the report as its brief.
	workerTask, review1 := seedReviewReport(t, issue, worker, runtimeID, "")
	dbfx.Exec(t, `DELETE FROM task_message WHERE task_id = $1`, review1) // storeCrossReviewReport writes it
	storeReviewOutput(t, review1, "Looks off.\n\n```review_report\n{\"verdict\":\"request_changes\",\"risks\":[\"retry path untested\"],\"questions\":[\"why a second queue?\"],\"suggestions\":[\"extract the helper\"],\"checklist_results\":[{\"item\":\"tests pass\",\"pass\":false,\"note\":\"no retry test\"}]}\n```")
	if n := countQueuedWorkerRuns(); n != 1 {
		t.Fatalf("queued worker runs after cycle 1 = %d, want 1", n)
	}
	var note string
	dbfx.QueryRow(t, `SELECT handoff_note FROM agent_task_queue WHERE issue_id = $1 AND review_of_task_id IS NULL AND status = 'queued'`, issue).Scan(&note)
	for _, want := range []string{"retry path untested", "why a second queue?", "extract the helper", "tests pass — no retry test"} {
		if !strings.Contains(note, want) {
			t.Fatalf("rework brief missing %q:\n%s", want, note)
		}
	}
	mu.Lock()
	if len(reworks) != 1 || reworks[0]["cycle"] != int64(1) {
		t.Fatalf("rework events = %+v", reworks)
	}
	mu.Unlock()

	// Cycle 2 hits max_cycles: no third run, the loop escalates instead.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND review_of_task_id IS NULL AND status = 'queued'`, issue)
	_, review2 := seedReviewReport(t, issue, worker, runtimeID, "")
	dbfx.Exec(t, `DELETE FROM task_message WHERE task_id = $1`, review2)
	storeReviewOutput(t, review2, "```review_report\n{\"verdict\":\"request_changes\",\"risks\":[\"still broken\"],\"questions\":[],\"suggestions\":[]}\n```")
	if n := countQueuedWorkerRuns(); n != 0 {
		t.Fatalf("queued worker runs past max_cycles = %d, want none", n)
	}
	mu.Lock()
	if len(escalations) != 1 {
		t.Fatalf("escalations = %+v", escalations)
	}
	if cycles, _ := escalations[0]["cycles"].(int64); cycles != 2 {
		t.Fatalf("escalation cycles = %+v", escalations[0])
	}
	mu.Unlock()

	// A done issue is never re-queued, whatever the verdict says.
	doneIssue := dbfx.Issue(t, "rework done issue", testutil.Cols{"status": "done", "project_id": project, "assignee_type": "agent", "assignee_id": worker})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE issue_id = $1)`, doneIssue)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, doneIssue)
	})
	_, doneReview := seedReviewReport(t, doneIssue, worker, runtimeID, "")
	dbfx.Exec(t, `DELETE FROM task_message WHERE task_id = $1`, doneReview)
	storeReviewOutput(t, doneReview, "```review_report\n{\"verdict\":\"request_changes\",\"risks\":[\"late review\"],\"questions\":[],\"suggestions\":[]}\n```")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND review_of_task_id IS NULL AND status = 'queued'`, doneIssue); n != 0 {
		t.Fatal("a done issue must not be reworked")
	}

	// Without the gate the report stays a plain signal: no rework.
	plain := dbfx.Project(t, "ungated rework project")
	reviewConfigCleanup(t, plain)
	plainIssue := dbfx.Issue(t, "ungated rework issue", testutil.Cols{"status": "in_progress", "project_id": plain, "assignee_type": "agent", "assignee_id": worker})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE issue_id = $1)`, plainIssue)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, plainIssue)
	})
	_, plainReview := seedReviewReport(t, plainIssue, worker, runtimeID, "")
	dbfx.Exec(t, `DELETE FROM task_message WHERE task_id = $1`, plainReview)
	storeReviewOutput(t, plainReview, "```review_report\n{\"verdict\":\"request_changes\",\"risks\":[\"no gate\"],\"questions\":[],\"suggestions\":[]}\n```")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND review_of_task_id IS NULL AND status = 'queued'`, plainIssue); n != 0 {
		t.Fatal("without gate_enabled a request_changes must not re-queue the worker")
	}
	_ = workerTask
}
