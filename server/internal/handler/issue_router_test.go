package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Issue router (K27), DB path: settings validation, a high-risk issue goes
// to the capable pool, repeated failures escalate a normal issue, the
// decision is readable per issue. Pure classification and counting live in
// internal/service/issue_router_test.go.

func TestIssueRouterSettingsAndRouting(t *testing.T) {
	rememberSettings(t)
	cheap, capable := poolRuntime(t, "route cheap", "online"), poolRuntime(t, "route capable", "online")
	cheapPool := dbfx.Insert(t, "runtime_pool", testutil.Cols{"workspace_id": testWorkspaceID, "name": "cheap", "runtime_ids": `["` + cheap + `"]`})
	capablePool := dbfx.Insert(t, "runtime_pool", testutil.Cols{"workspace_id": testWorkspaceID, "name": "capable", "runtime_ids": `["` + capable + `"]`})
	project := dbfx.Project(t, "routing project")
	agent := dbfx.Agent(t, "route agent", cheap)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
		testPool.Exec(context.Background(), `DELETE FROM runtime_pool WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM project_blast_radius_rule WHERE project_id = $1`, project)
	})
	// Settings: unknown level and foreign pool are refused; a valid policy lands.
	poolCall(t, testHandler.PutRoutingSettings, http.MethodPut, "/api/routing-settings", map[string]any{"enabled": true, "pools": map[string]string{"weird": cheapPool}}).Want(http.StatusBadRequest)
	poolCall(t, testHandler.PutRoutingSettings, http.MethodPut, "/api/routing-settings", map[string]any{"enabled": true, "pools": map[string]string{"low": "00000000-0000-0000-0000-000000000001"}}).Want(http.StatusUnprocessableEntity)
	var cfg service.Routing
	poolCall(t, testHandler.PutRoutingSettings, http.MethodPut, "/api/routing-settings", map[string]any{"enabled": true, "pools": map[string]string{"low": cheapPool, "normal": cheapPool, "high": capablePool}, "escalation_failures": 2}).Want(http.StatusOK).JSON(&cfg)
	poolCall(t, testHandler.GetRoutingSettings, http.MethodGet, "/api/routing-settings", nil).Want(http.StatusOK).JSON(&cfg)
	if !cfg.Enabled || cfg.Pools["high"] != capablePool || cfg.EscalationFailures != 2 {
		t.Fatalf("settings = %+v", cfg)
	}
	blastCall(t, testHandler.CreateBlastRadiusRule, http.MethodPost, "/api/projects/"+project+"/blast-radius-rules", map[string]any{"path_pattern": "billing/**", "autonomy_level": "dual_approval"}, "id", project).Want(http.StatusCreated)

	ctx := context.Background()
	enqueue := func(issueID string) service.RoutingDecision {
		t.Helper()
		issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil {
			t.Fatal(err)
		}
		task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		var d service.RoutingDecision
		if err := json.Unmarshal(task.RoutingDecision, &d); err != nil {
			t.Fatalf("decision missing on task: %s", task.RoutingDecision)
		}
		if d.RuntimeID != uuidToString(task.RuntimeID) {
			t.Fatalf("task runtime %s must be the routed one %s", uuidToString(task.RuntimeID), d.RuntimeID)
		}
		return d
	}
	// A high-risk issue (dual approval path) never goes to the cheap pool.
	high := dbfx.Issue(t, "Refund flow in billing/ledger.go", testutil.Cols{"status": "todo", "assignee_type": "agent", "assignee_id": agent, "project_id": project})
	if d := enqueue(high); d.RiskLevel != service.RiskHigh || d.TargetPoolID != capablePool || d.RuntimeID != capable || d.Escalated {
		t.Fatalf("high decision = %+v", d)
	}
	// A normal issue goes cheap, then escalates after two consecutive failures.
	normal := dbfx.Issue(t, "Tweak docs wording", testutil.Cols{"status": "todo", "assignee_type": "agent", "assignee_id": agent, "project_id": project})
	if d := enqueue(normal); d.RiskLevel != service.RiskNormal || d.TargetPoolID != cheapPool || d.Escalated {
		t.Fatalf("normal decision = %+v", d)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'agent_error', completed_at = now() WHERE issue_id = $1`, normal)
	if d := enqueue(normal); d.Escalated {
		t.Fatalf("one failure must not escalate: %+v", d)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'agent_error', completed_at = now() WHERE issue_id = $1`, normal)
	d := enqueue(normal)
	if !d.Escalated || d.RiskLevel != service.RiskHigh || d.TargetPoolID != capablePool || d.RuntimeID != capable {
		t.Fatalf("escalated decision = %+v", d)
	}
	var out struct {
		Decision service.RoutingDecision `json:"decision"`
		TaskID   string                  `json:"task_id"`
	}
	testutil.Call(t, testHandler.GetIssueRoutingDecision, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+normal+"/routing-decision", nil), "id", normal)).Want(http.StatusOK).JSON(&out)
	if !out.Decision.Escalated || out.Decision.EscalationReason == "" || out.TaskID == "" {
		t.Fatalf("routing decision endpoint = %+v", out)
	}
	// Routing off: the agent's own runtime, no decision.
	poolCall(t, testHandler.PutRoutingSettings, http.MethodPut, "/api/routing-settings", map[string]any{"enabled": false}).Want(http.StatusOK)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'cancelled', completed_at = now() WHERE issue_id = $1`, high)
	issue, _ := testHandler.Queries.GetIssue(ctx, parseUUID(high))
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.RoutingDecision) != 0 || uuidToString(task.RuntimeID) != cheap {
		t.Fatalf("routing off: decision %s runtime %s", task.RoutingDecision, uuidToString(task.RuntimeID))
	}
}
