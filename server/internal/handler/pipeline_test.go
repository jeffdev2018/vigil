package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Pipelines (K37): stages validated against the workspace, one open run
// per issue, automatic advance when the executor's run completes, a gated
// stage waits on a Decision Card and moves on approval, a vanished
// executor pauses the run with an explicit error, stages cannot change
// under an open run, cancel.

func pipelineCall(t *testing.T, h http.HandlerFunc, method, path string, body any, params ...string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, h, testutil.WithURLParams(testutil.WithHeaders(newRequest(method, path, body), "X-Workspace-ID", testWorkspaceID), params...))
}

func TestPipelineRunsThroughStagesWithAGate(t *testing.T) {
	planner := dbfx.Agent(t, "pipeline planner", handlerTestRuntimeID(t))
	builder := dbfx.Agent(t, "pipeline builder", handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "pipeline issue", testutil.Cols{"status": "todo"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM pipeline_run WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM pipeline_stage WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM pipeline WHERE workspace_id = $1`, testWorkspaceID)
	})
	ctx := context.Background()
	stages := []map[string]any{
		{"name": "plan", "executor_type": "agent", "executor_id": planner},
		{"name": "implement", "executor_type": "agent", "executor_id": builder, "requires_human_gate": true},
	}
	pipelineCall(t, testHandler.CreatePipeline, http.MethodPost, "/api/pipelines", map[string]any{"name": "flow", "stages": []map[string]any{{"name": "x", "executor_type": "agent", "executor_id": "00000000-0000-0000-0000-000000000001"}}}).Want(http.StatusUnprocessableEntity)
	var pipeline PipelineResponse
	pipelineCall(t, testHandler.CreatePipeline, http.MethodPost, "/api/pipelines", map[string]any{"name": "flow", "stages": stages}).Want(http.StatusCreated).JSON(&pipeline)
	if len(pipeline.Stages) != 2 || pipeline.Stages[1].Position != 1 || !pipeline.Stages[1].RequiresHumanGate {
		t.Fatalf("pipeline = %+v", pipeline)
	}
	var out struct{ Run *PipelineRunResponse }
	testutil.Call(t, testHandler.StartPipelineRun, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issue+"/pipeline-run", map[string]any{"pipeline_id": pipeline.ID}), "id", issue)).Want(http.StatusCreated).JSON(&out)
	if out.Run == nil || out.Run.Status != "active" || out.Run.CurrentIndex != 0 {
		t.Fatalf("started run = %+v", out.Run)
	}
	// Stage 1: the issue went to the planner and a run is queued for it.
	issueRow, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue))
	if uuidToString(issueRow.AssigneeID) != planner {
		t.Fatalf("issue assignee = %s, want planner", uuidToString(issueRow.AssigneeID))
	}
	var planTask string
	testPool.QueryRow(ctx, `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 ORDER BY created_at DESC LIMIT 1`, issue, planner).Scan(&planTask)
	if planTask == "" {
		t.Fatal("no run queued for the planner")
	}
	// One open run per issue; stages are frozen while it is open.
	testutil.Call(t, testHandler.StartPipelineRun, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issue+"/pipeline-run", map[string]any{"pipeline_id": pipeline.ID}), "id", issue)).Want(http.StatusConflict)
	pipelineCall(t, testHandler.UpdatePipeline, http.MethodPatch, "/api/pipelines/"+pipeline.ID, map[string]any{"stages": stages}, "id", pipeline.ID).Want(http.StatusConflict)
	pipelineCall(t, testHandler.UpdatePipeline, http.MethodPatch, "/api/pipelines/"+pipeline.ID, map[string]any{"name": "flow v2"}, "id", pipeline.ID).Want(http.StatusOK)
	// The planner's run completes: the next stage is gated, so a Decision Card asks.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, planTask)
	testHandler.advancePipelineAfterTask(ctx, mustTask(t, planTask))
	testutil.Call(t, testHandler.GetIssuePipelineRun, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/pipeline-run", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
	if out.Run.Status != "paused" || out.Run.GateDecisionID == nil || out.Run.CurrentIndex != 1 {
		t.Fatalf("gated run = %+v", out.Run)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issue, builder); n != 0 {
		t.Fatal("the gated stage must not start before approval")
	}
	// Approval on the card moves the issue to the builder.
	respondDecision(t, issue, *out.Run.GateDecisionID, map[string]any{"option_id": pipelineApproveOption}).Want(http.StatusOK)
	testutil.Call(t, testHandler.GetIssuePipelineRun, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/pipeline-run", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
	if out.Run.Status != "active" || out.Run.CurrentIndex != 1 || out.Run.GateDecisionID != nil {
		t.Fatalf("approved run = %+v", out.Run)
	}
	issueRow, _ = testHandler.Queries.GetIssue(ctx, parseUUID(issue))
	var buildTask string
	testPool.QueryRow(ctx, `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 ORDER BY created_at DESC LIMIT 1`, issue, builder).Scan(&buildTask)
	if uuidToString(issueRow.AssigneeID) != builder || buildTask == "" {
		t.Fatal("approval must hand the issue to the builder and queue its run")
	}
	// A foreign run completing does not advance; the builder's does, and the last stage completes the pipeline.
	testHandler.advancePipelineAfterTask(ctx, mustTask(t, planTask))
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, buildTask)
	testHandler.advancePipelineAfterTask(ctx, mustTask(t, buildTask))
	testutil.Call(t, testHandler.GetIssuePipelineRun, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/pipeline-run", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
	if out.Run.Status != "completed" || out.Run.CurrentIndex != 2 || out.Run.CompletedAt == nil {
		t.Fatalf("finished run = %+v", out.Run)
	}
	// Stages can change again, and the pipeline can be archived.
	pipelineCall(t, testHandler.UpdatePipeline, http.MethodPatch, "/api/pipelines/"+pipeline.ID, map[string]any{"stages": stages[:1]}, "id", pipeline.ID).Want(http.StatusOK).JSON(&pipeline)
	if len(pipeline.Stages) != 1 {
		t.Fatalf("stages after edit = %d", len(pipeline.Stages))
	}
	pipelineCall(t, testHandler.DeletePipeline, http.MethodDelete, "/api/pipelines/"+pipeline.ID, nil, "id", pipeline.ID).Want(http.StatusNoContent)
}

func TestPipelineVanishedExecutorPausesWithAnExplicitError(t *testing.T) {
	gone := dbfx.Agent(t, "pipeline gone", handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "pipeline gone issue", testutil.Cols{"status": "todo"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM pipeline_run WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM pipeline_stage WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM pipeline WHERE workspace_id = $1`, testWorkspaceID)
	})
	var pipeline PipelineResponse
	pipelineCall(t, testHandler.CreatePipeline, http.MethodPost, "/api/pipelines", map[string]any{"name": "fragile", "stages": []map[string]any{{"name": "only", "executor_type": "agent", "executor_id": gone}}}).Want(http.StatusCreated).JSON(&pipeline)
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, gone)
	var out struct{ Run *PipelineRunResponse }
	testutil.Call(t, testHandler.StartPipelineRun, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issue+"/pipeline-run", map[string]any{"pipeline_id": pipeline.ID}), "id", issue)).Want(http.StatusCreated).JSON(&out)
	if out.Run.Status != "paused" || out.Run.LastError == nil {
		t.Fatalf("run with a vanished executor = %+v", out.Run)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'pipeline' AND severity = 'action_required'`, issue); n < 1 {
		t.Fatal("the stop must reach the attention inbox")
	}
	// Fixing the agent and advancing retries the stage; cancel ends the run.
	dbfx.Exec(t, `UPDATE agent SET archived_at = NULL WHERE id = $1`, gone)
	testutil.Call(t, testHandler.AdvancePipelineRun, testutil.WithURLParams(newRequest(http.MethodPost, "/api/pipeline-runs/"+out.Run.ID+"/advance", nil), "id", out.Run.ID)).Want(http.StatusOK).JSON(&out)
	if out.Run.Status != "active" || out.Run.LastError != nil {
		t.Fatalf("retried run = %+v", out.Run)
	}
	testutil.Call(t, testHandler.CancelPipelineRun, testutil.WithURLParams(newRequest(http.MethodPost, "/api/pipeline-runs/"+out.Run.ID+"/cancel", nil), "id", out.Run.ID)).Want(http.StatusOK).JSON(&out)
	if out.Run.Status != "cancelled" {
		t.Fatalf("cancelled run = %+v", out.Run)
	}
}
