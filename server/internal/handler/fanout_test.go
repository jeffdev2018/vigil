package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Fan-out / fan-in (K38): N sub-tasks become N child issues with their own
// runs; a second fan-out on the same issue is refused; the barrier waits
// for every child (a failed child with a retry pending is not final); the
// synthesis run starts for the leader with every outcome in its handoff,
// in partial_failure when a child failed for good.

func TestFanoutBarrierAndSynthesis(t *testing.T) {
	leader := dbfx.Agent(t, "fanout leader", handlerTestRuntimeID(t))
	specA := dbfx.Agent(t, "fanout spec a", handlerTestRuntimeID(t))
	specB := dbfx.Agent(t, "fanout spec b", handlerTestRuntimeID(t))
	parent := dbfx.Issue(t, "Ship the release", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": leader})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2, $3)`, leader, specA, specB)
		testPool.Exec(context.Background(), `DELETE FROM fanout_batch_member WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM fanout_batch WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE parent_issue_id = $1`, parent)
	})
	ctx := context.Background()
	// Fixture issues do not bump the workspace counter; real creation does.
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)
	body := map[string]any{"leader_agent_id": leader, "sub_tasks": []map[string]any{{"description": "Write the changelog", "assignee_id": specA}, {"description": "Bump the version\nand tag", "assignee_id": specB}}}
	var out struct{ Batch *FanoutBatchResponse }
	testutil.Call(t, testHandler.StartFanout, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+parent+"/fanout", map[string]any{"leader_agent_id": leader, "sub_tasks": []map[string]any{{"description": "x", "assignee_id": "00000000-0000-0000-0000-000000000001"}}}), "id", parent)).Want(http.StatusUnprocessableEntity)
	testutil.Call(t, testHandler.StartFanout, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+parent+"/fanout", body), "id", parent)).Want(http.StatusCreated).JSON(&out)
	if out.Batch == nil || out.Batch.Status != "pending" || out.Batch.ExpectedCount != 2 || len(out.Batch.Members) != 2 {
		t.Fatalf("batch = %+v", out.Batch)
	}
	if res := testutil.Call(t, testHandler.StartFanout, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+parent+"/fanout", body), "id", parent)).Want(http.StatusConflict); res.Map()["code"] != ErrCodeFanoutActive {
		t.Fatalf("second fan-out = %v", res.Map())
	}
	// Each child is a real issue under the parent, assigned to its specialist, with a queued run.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE parent_issue_id = $1 AND assignee_type = 'agent'`, parent); n != 2 {
		t.Fatalf("child issues = %d", n)
	}
	var titleB string
	testPool.QueryRow(ctx, `SELECT title FROM issue WHERE parent_issue_id = $1 AND assignee_id = $2`, parent, specB).Scan(&titleB)
	if titleB != "Bump the version" {
		t.Fatalf("child title = %q, want the first line", titleB)
	}
	mA, mB := out.Batch.Members[0], out.Batch.Members[1]
	if mA.AssigneeAgentID == specB {
		mA, mB = mB, mA
	}
	// A completes: the barrier moves, nothing else.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, mA.TaskID)
	testHandler.updateFanoutBarrier(ctx, mustTask(t, mA.TaskID))
	testutil.Call(t, testHandler.GetIssueFanout, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+parent+"/fanout", nil), "id", parent)).Want(http.StatusOK).JSON(&out)
	if out.Batch.Status != "pending" || out.Batch.CompletedCount != 1 || out.Batch.SynthesisTaskID != nil {
		t.Fatalf("after A = %+v", out.Batch)
	}
	// B fails but a retry is pending: not final.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'runtime_offline', completed_at = now() WHERE id = $1`, mB.TaskID)
	retry := dbfx.Task(t, specB, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": mB.ChildIssueID, "status": "queued", "parent_task_id": mB.TaskID, "retry_of_task_id": mB.TaskID})
	testHandler.updateFanoutBarrier(ctx, mustTask(t, mB.TaskID))
	testutil.Call(t, testHandler.GetIssueFanout, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+parent+"/fanout", nil), "id", parent)).Want(http.StatusOK).JSON(&out)
	if out.Batch.Status != "pending" || out.Batch.FailedCount != 0 {
		t.Fatalf("a failed child with a retry pending must not settle: %+v", out.Batch)
	}
	// The retry fails for good: partial_failure, synthesis queued for the leader with the outcomes.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'agent_error', completed_at = now() WHERE id = $1`, retry)
	testHandler.updateFanoutBarrier(ctx, mustTask(t, retry))
	testutil.Call(t, testHandler.GetIssueFanout, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+parent+"/fanout", nil), "id", parent)).Want(http.StatusOK).JSON(&out)
	if out.Batch.Status != "partial_failure" || out.Batch.FailedCount != 1 || out.Batch.CompletedCount != 1 || out.Batch.SynthesisTaskID == nil {
		t.Fatalf("settled batch = %+v", out.Batch)
	}
	synthesis := mustTask(t, *out.Batch.SynthesisTaskID)
	if uuidToString(synthesis.AgentID) != leader || uuidToString(synthesis.IssueID) != parent || synthesis.Status != "queued" {
		t.Fatalf("synthesis = agent %s issue %s status %s", uuidToString(synthesis.AgentID), uuidToString(synthesis.IssueID), synthesis.Status)
	}
	note := synthesis.HandoffNote.String
	for _, want := range []string{"WARNING", "[completed] Write the changelog", "[failed] Bump the version"} {
		if !strings.Contains(note, want) {
			t.Fatalf("synthesis handoff lacks %q:\n%s", want, note)
		}
	}
	// A second fan-out is allowed once the batch settled.
	testutil.Call(t, testHandler.StartFanout, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+parent+"/fanout", map[string]any{"leader_agent_id": leader, "sub_tasks": []map[string]any{{"description": "Again", "assignee_id": specA}}}), "id", parent)).Want(http.StatusCreated)
	var batchOut struct{ Batch *FanoutBatchResponse }
	testutil.Call(t, testHandler.GetFanoutBatch, testutil.WithURLParams(newRequest(http.MethodGet, "/api/fanout-batches/"+out.Batch.ID, nil), "id", out.Batch.ID)).Want(http.StatusOK).JSON(&batchOut)
	if batchOut.Batch == nil || batchOut.Batch.ID != out.Batch.ID {
		t.Fatal("batch endpoint")
	}
}
