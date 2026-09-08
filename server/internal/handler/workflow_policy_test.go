package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Workflow selector handler tests (JEF-273): the workspace policy endpoint,
// the 90-day workflow-stats rollup, the workflow field on the task payload,
// and the critique workflow's forced cross-review.

func TestWorkflowPolicySettings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rememberSettings(t)

	// Default: off.
	var got map[string]any
	testutil.Call(t, testHandler.GetWorkflowPolicySettings, newRequest(http.MethodGet, "/api/workflow-policy-settings", nil)).Want(http.StatusOK).JSON(&got)
	if got["mode"] != "off" {
		t.Fatalf("default settings = %v, want mode off", got)
	}

	// Unknown mode → 400.
	testutil.Call(t, testHandler.PutWorkflowPolicySettings, newRequest(http.MethodPut, "/api/workflow-policy-settings", map[string]any{"mode": "smart"})).Want(http.StatusBadRequest)

	// Valid mode → 200, persisted, and the workspace's other settings survive.
	testutil.Call(t, testHandler.PutWorkflowPolicySettings, newRequest(http.MethodPut, "/api/workflow-policy-settings", map[string]any{"mode": "auto"})).Want(http.StatusOK).JSON(&got)
	if got["mode"] != "auto" {
		t.Fatalf("after PUT settings = %v, want mode auto", got)
	}
	got = map[string]any{}
	testutil.Call(t, testHandler.GetWorkflowPolicySettings, newRequest(http.MethodGet, "/api/workflow-policy-settings", nil)).Want(http.StatusOK).JSON(&got)
	if got["mode"] != "auto" {
		t.Fatalf("GET after PUT = %v, want mode auto", got)
	}
}

func TestGetWorkflowStats(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "workflow-stats-agent", nil)
	runtimeID := handlerTestRuntimeID(t)
	seed := func(workflow string, samples, successes int, costTicks int64) {
		for i := 0; i < samples; i++ {
			status := "failed"
			if i < successes {
				status = "completed"
			}
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id":   runtimeID,
				"status":       status,
				"task_class":   "bugfix",
				"context":      `{"workflow":"` + workflow + `"}`,
				"started_at":   testutil.Raw("now() - interval '2 minutes'"),
				"completed_at": testutil.Raw("now() - interval '1 minute'"),
			})
			dbfx.Insert(t, "task_usage", testutil.Cols{
				"task_id":        taskID,
				"provider":       "openai",
				"model":          "gpt-test",
				"cost_usd_ticks": costTicks,
			})
		}
	}
	seed("cascade", 4, 3, 1_000_000_000) // $0.10
	seed("critique", 2, 2, 5_000_000_000)
	// A run with no context stamp at all lands in the single bucket.
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"status":       "completed",
		"task_class":   "bugfix",
		"started_at":   testutil.Raw("now() - interval '2 minutes'"),
		"completed_at": testutil.Raw("now() - interval '1 minute'"),
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": taskID, "provider": "openai", "model": "gpt-test", "cost_usd_ticks": 500_000_000})

	resp := testutil.Decode[WorkflowStatsResponse](t, testHandler.GetWorkflowStats, newRequest(http.MethodGet, "/api/runtimes/workflow-stats", nil), http.StatusOK)
	if resp.WindowDays != workflowStatsWindowDays {
		t.Errorf("window_days = %d, want %d", resp.WindowDays, workflowStatsWindowDays)
	}
	find := func(workflow string) *WorkflowStatsRow {
		for i := range resp.Rows {
			if r := &resp.Rows[i]; r.TaskClass == "bugfix" && r.Workflow == workflow {
				return r
			}
		}
		return nil
	}
	cascade := find("cascade")
	if cascade == nil || cascade.Samples != 4 || cascade.SuccessRate != 0.75 {
		t.Fatalf("cascade row = %+v", cascade)
	}
	if cascade.AvgCostUSD == nil || *cascade.AvgCostUSD != 0.10 {
		t.Errorf("cascade avg_cost_usd = %v, want 0.10", cascade.AvgCostUSD)
	}
	if cascade.AvgDurationSecs == nil || *cascade.AvgDurationSecs != 60 {
		t.Errorf("cascade avg_duration_secs = %v, want 60", cascade.AvgDurationSecs)
	}
	if find("critique") == nil || find("critique").Samples != 2 {
		t.Errorf("critique row = %+v", find("critique"))
	}
	// The unstamped run defaults into the single bucket. Other tests of this
	// package may add single rows of their own, so only assert presence.
	if find("single") == nil {
		t.Error("no single row — the unstamped run did not default to single")
	}
}

// taskToResponse surfaces the workflow stamp from the task context.
func TestTaskToResponseWorkflow(t *testing.T) {
	withStamp := taskToResponse(db.AgentTaskQueue{Context: []byte(`{"workflow":"cascade","force_review":true}`)}, "")
	if withStamp.Workflow != "cascade" {
		t.Errorf("workflow = %q, want cascade", withStamp.Workflow)
	}
	if bare := taskToResponse(db.AgentTaskQueue{}, ""); bare.Workflow != "" {
		t.Errorf("workflow = %q, want empty on an unstamped row", bare.Workflow)
	}
	if corrupt := taskToResponse(db.AgentTaskQueue{Context: []byte("{corrupt")}, ""); corrupt.Workflow != "" {
		t.Errorf("workflow = %q, want empty on a corrupt context", corrupt.Workflow)
	}
}

// A critique-workflow run (force_review in its context stamp) gets its
// cross-review even with the workspace policy switched off; a plain run under
// the same policy does not.
func TestCrossReviewForcedByWorkflowStamp(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	claude := providerRuntime(t, "claude")
	codex := providerRuntime(t, "codex")
	author := dbfx.Agent(t, "wf xr author", claude)
	reviewer := dbfx.Agent(t, "wf xr reviewer", codex)
	issue := dbfx.Issue(t, "Forced review issue")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, author, reviewer)
	})
	rememberSettings(t)
	quietOtherAgents(t, author, reviewer)
	prev := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{diff: "diff --git a/x.go b/x.go\n+fmt.Println(1)\n"}
	t.Cleanup(func() { testHandler.DiffFetcher = prev })

	// Workspace-wide off: the policy guard is the one force_review bypasses.
	testutil.Call(t, testHandler.PutCrossReviewSettings, newRequest(http.MethodPut, "/api/cross-review-settings", map[string]any{"enabled": false, "opt_out_project_ids": []string{}})).Want(http.StatusOK)

	forced := dbfx.Task(t, author, testutil.Cols{
		"runtime_id": claude, "issue_id": issue, "status": "completed",
		"completed_at": testutil.Raw("now()"),
		"context":      `{"workflow":"critique","force_review":true}`,
	})
	testHandler.triggerCrossReview(context.Background(), mustTask(t, forced), "https://github.com/org/repo/pull/11", "")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE review_of_task_id = $1`, forced); n != 1 {
		t.Fatalf("reviews of the forced run = %d, want 1 (force_review bypasses the disabled policy)", n)
	}

	plain := dbfx.Task(t, author, testutil.Cols{
		"runtime_id": claude, "issue_id": issue, "status": "completed",
		"completed_at": testutil.Raw("now()"),
		"context":      `{"workflow":"single"}`,
	})
	testHandler.triggerCrossReview(context.Background(), mustTask(t, plain), "https://github.com/org/repo/pull/12", "")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE review_of_task_id = $1`, plain); n != 0 {
		t.Fatal("a run without force_review stays unreviewed while the policy is off")
	}
}
