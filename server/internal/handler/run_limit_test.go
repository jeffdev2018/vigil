package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Run limits (K03), DB path: CRUD with scope validation and one policy per
// scope, warn once at the threshold, observe records without stopping,
// enforce stops the run with its own reason and one event, the duration
// cap moves with the sweeper, status is readable per run and per issue.

func TestRunLimitPoliciesCRUD(t *testing.T) {
	agent := dbfx.Agent(t, "limit crud agent", handlerTestRuntimeID(t))
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM run_limit_policy WHERE workspace_id = $1`, testWorkspaceID)
	})
	poolCall(t, testHandler.CreateRunLimitPolicy, http.MethodPost, "/api/run-limits", map[string]any{"scope_type": "workspace", "action": "enforce"}).Want(http.StatusBadRequest)
	poolCall(t, testHandler.CreateRunLimitPolicy, http.MethodPost, "/api/run-limits", map[string]any{"scope_type": "agent", "scope_id": "00000000-0000-0000-0000-000000000001", "max_turns": 5}).Want(http.StatusBadRequest)
	var p RunLimitPolicyResponse
	poolCall(t, testHandler.CreateRunLimitPolicy, http.MethodPost, "/api/run-limits", map[string]any{"scope_type": "agent", "scope_id": agent, "max_turns": 5, "max_cost_usd_ticks": 30000000000, "warn_bps": 5000, "action": "observe"}).Want(http.StatusCreated).JSON(&p)
	if p.MaxTurns == nil || *p.MaxTurns != 5 || p.MaxDurationSeconds != nil || p.WarnBps != 5000 || p.Action != "observe" {
		t.Fatalf("policy = %+v", p)
	}
	poolCall(t, testHandler.CreateRunLimitPolicy, http.MethodPost, "/api/run-limits", map[string]any{"scope_type": "agent", "scope_id": agent, "max_turns": 9}).Want(http.StatusConflict)
	poolCall(t, testHandler.UpdateRunLimitPolicy, http.MethodPut, "/api/run-limits/"+p.ID, map[string]any{"max_duration_seconds": 120, "action": "enforce"}, "id", p.ID).Want(http.StatusOK).JSON(&p)
	if p.MaxDurationSeconds == nil || *p.MaxDurationSeconds != 120 || p.MaxTurns != nil || p.Action != "enforce" || p.WarnBps != 5000 {
		t.Fatalf("updated = %+v, want caps replaced and warn kept", p)
	}
	viewer := dbfx.User(t, "Limit viewer", "limit-viewer@multica.ai")
	dbfx.Member(t, testWorkspaceID, viewer, "member")
	req := testutil.WithHeaders(newRequest(http.MethodDelete, "/api/run-limits/"+p.ID, nil), "X-Workspace-ID", testWorkspaceID, "X-User-ID", viewer)
	testutil.Call(t, testHandler.DeleteRunLimitPolicy, testutil.WithURLParams(req, "id", p.ID)).Want(http.StatusForbidden)
	var list struct{ Policies []RunLimitPolicyResponse }
	poolCall(t, testHandler.ListRunLimitPolicies, http.MethodGet, "/api/run-limits", nil).Want(http.StatusOK).JSON(&list)
	if len(list.Policies) != 1 {
		t.Fatalf("list = %+v", list.Policies)
	}
	poolCall(t, testHandler.DeleteRunLimitPolicy, http.MethodDelete, "/api/run-limits/"+p.ID, nil, "id", p.ID).Want(http.StatusNoContent)
}

func TestRunLimitsWarnObserveAndStop(t *testing.T) {
	issue, task, agent := runningAgentRun(t, "run limit")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM run_limit_event WHERE task_id = $1`, task)
		testPool.Exec(context.Background(), `DELETE FROM run_limit_policy WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE type LIKE 'run_limit_%' AND workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM task_usage WHERE task_id = $1`, task)
		testPool.Exec(context.Background(), `DELETE FROM task_message WHERE task_id = $1`, task)
	})
	ctx := context.Background()
	// Workspace: 10 turns observed; agent: $1 enforced with a 50% warning. The agent's cost cap and the workspace's turn cap both apply.
	poolCall(t, testHandler.CreateRunLimitPolicy, http.MethodPost, "/api/run-limits", map[string]any{"scope_type": "workspace", "max_turns": 10, "action": "observe"}).Want(http.StatusCreated)
	poolCall(t, testHandler.CreateRunLimitPolicy, http.MethodPost, "/api/run-limits", map[string]any{"scope_type": "agent", "scope_id": agent, "max_cost_usd_ticks": 10000000000, "warn_bps": 5000, "action": "enforce"}).Want(http.StatusCreated)
	load := func() db.AgentTaskQueue { return mustTask(t, task) }
	// Below every threshold: nothing.
	if testHandler.TaskService.EvaluateRunLimits(ctx, load()) {
		t.Fatal("nothing consumed must not stop")
	}
	// $0.60: past the 50% warning once, even when evaluated twice.
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": task, "provider": "claude", "model": "m", "input_tokens": 1, "output_tokens": 1, "cache_read_tokens": 0, "cache_write_tokens": 0, "cost_usd_ticks": 6000000000})
	testHandler.TaskService.EvaluateRunLimits(ctx, load())
	testHandler.TaskService.EvaluateRunLimits(ctx, load())
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM run_limit_event WHERE task_id = $1 AND gate = 'cost' AND level = 'warn'`, task); n != 1 {
		t.Fatalf("warn events = %d, want exactly one", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = 'run_limit_warn' AND details->>'task_id' = $1`, task); n < 1 {
		t.Fatal("the warning must reach a manager's inbox")
	}
	// 12 turns: the observed workspace cap records, never stops.
	for i := 0; i < 12; i++ {
		dbfx.Insert(t, "task_message", testutil.Cols{"task_id": task, "seq": i + 1, "type": "text", "content": "turn"})
	}
	if testHandler.TaskService.EvaluateRunLimits(ctx, load()) || load().Status != "running" {
		t.Fatal("observe must never stop the run")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM run_limit_event WHERE task_id = $1 AND gate = 'turns' AND level = 'exceeded'`, task); n != 1 {
		t.Fatalf("exceeded events = %d, want one", n)
	}
	// Status endpoint: the run's own credential and a member both read it, values never leak beyond numbers.
	var status struct {
		Usage  service.RunUsage        `json:"usage"`
		Gates  []service.RunLimitGate  `json:"gates"`
		Events []RunLimitEventResponse `json:"events"`
	}
	gateCall(t, testHandler.GetTaskBudgetStatus, http.MethodGet, "/api/tasks/"+task+"/budget-status", nil, gateHeaders(task, agent), "taskId", task).Want(http.StatusOK).JSON(&status)
	if status.Usage.Turns != 12 || status.Usage.CostUsdTicks != 6000000000 || len(status.Gates) != 2 || len(status.Events) != 2 {
		t.Fatalf("status = %+v", status)
	}
	// $1.20: the enforced cap stops the run with its own reason, once.
	dbfx.Exec(t, `UPDATE task_usage SET cost_usd_ticks = 12000000000 WHERE task_id = $1`, task)
	if !testHandler.TaskService.EvaluateRunLimits(ctx, load()) {
		t.Fatal("an enforced cap at 120% must stop the run")
	}
	after := load()
	if after.Status != "failed" || after.FailureReason.String != service.ReasonBudgetExceeded {
		t.Fatalf("task after stop = %s / %s", after.Status, after.FailureReason.String)
	}
	if testHandler.TaskService.EvaluateRunLimits(ctx, after) {
		t.Fatal("a stopped run is left alone")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM run_limit_event WHERE task_id = $1 AND level = 'stopped'`, task); n != 1 {
		t.Fatalf("stopped events = %d, want one", n)
	}
	var events struct{ Events []RunLimitEventResponse }
	testutil.Call(t, testHandler.ListIssueRunLimitEvents, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/run-limit-events", nil), "id", issue)).Want(http.StatusOK).JSON(&events)
	if len(events.Events) != 3 || events.Events[0].Level != "stopped" {
		t.Fatalf("issue events = %+v", events.Events)
	}
}

func TestRunLimitDurationSweep(t *testing.T) {
	_, task, agent := runningAgentRun(t, "run limit duration")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM run_limit_event WHERE task_id = $1`, task)
		testPool.Exec(context.Background(), `DELETE FROM run_limit_policy WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE type LIKE 'run_limit_%' AND workspace_id = $1`, testWorkspaceID)
	})
	poolCall(t, testHandler.CreateRunLimitPolicy, http.MethodPost, "/api/run-limits", map[string]any{"scope_type": "agent", "scope_id": agent, "max_duration_seconds": 60, "action": "enforce"}).Want(http.StatusCreated)
	dbfx.Exec(t, `UPDATE agent_task_queue SET started_at = now() - interval '2 minutes' WHERE id = $1`, task)
	if stopped := testHandler.TaskService.SweepRunLimits(context.Background(), 100); stopped < 1 {
		t.Fatalf("sweep stopped %d runs, want the overdue one", stopped)
	}
	if got := mustTask(t, task); got.Status != "failed" || got.FailureReason.String != service.ReasonBudgetExceeded {
		t.Fatalf("overdue run = %s / %s", got.Status, got.FailureReason.String)
	}
}
