package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Per-leg accounting (JEF-274): GET /api/tasks/{taskId}/legs answers with the
// whole workflow — the primary run plus every leg that derives from it — and
// the totals every leg contributed. Reachable from ANY leg, because a client
// looking at a review or a retry has no reason to know the draft's id.
//
// The routing half of the feature lives in TestTaskLegsKeepReviewOutOfRoutingStats
// below: a review-like leg must not be counted as a sample of the worker's
// task class, while a retry must.

// legFixture builds one four-leg workflow on a fresh issue: a primary run,
// its review, the revision the review asked for, and a retry of the primary
// that was moved to another runtime. Each leg carries a distinct cost so a
// wrong total names the leg it dropped.
type legFixture struct {
	root, review, revision, retry string
}

func seedWorkflow(t *testing.T) legFixture {
	t.Helper()

	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "task-legs-agent", nil)
	issueID := dbfx.Issue(t, "Task legs issue", testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
	})

	leg := func(role, root string, cost int64, over testutil.Cols) string {
		cols := testutil.Cols{
			"runtime_id":   runtimeID,
			"issue_id":     issueID,
			"status":       "completed",
			"leg_role":     role,
			"started_at":   testutil.Raw("now() - interval '2 minutes'"),
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
		}
		if root != "" {
			cols["workflow_root_task_id"] = root
		}
		for k, v := range over {
			cols[k] = v
		}
		id := dbfx.Task(t, agentID, cols)
		dbfx.Insert(t, "task_usage", testutil.Cols{
			"task_id":        id,
			"provider":       "openai",
			"model":          "task-legs-model",
			"input_tokens":   100,
			"output_tokens":  10,
			"cost_usd_ticks": cost,
		})
		return id
	}

	root := leg("", "", 1_000_000_000, nil) // the primary leg carries no role
	return legFixture{
		root:     root,
		review:   leg(service.LegRoleReview, root, 2_000_000_000, nil),
		revision: leg(service.LegRoleRevision, root, 3_000_000_000, nil),
		retry:    leg(service.LegRoleFallback, root, 4_000_000_000, nil),
	}
}

func fetchLegs(t *testing.T, taskID string) WorkflowLegsResponse {
	t.Helper()
	return testutil.Decode[WorkflowLegsResponse](t, testHandler.GetTaskLegs,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/tasks/"+taskID+"/legs", nil), "taskId", taskID),
		http.StatusOK)
}

func TestTaskLegsReachableFromAnyLeg(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	f := seedWorkflow(t)
	wantRoles := map[string]string{
		f.root:     legRoleDraft,
		f.review:   service.LegRoleReview,
		f.revision: service.LegRoleRevision,
		f.retry:    service.LegRoleFallback,
	}

	for _, from := range []string{f.root, f.review, f.revision, f.retry} {
		body := fetchLegs(t, from)
		if body.RootTaskID != f.root {
			t.Errorf("from %s: root_task_id = %q, want the primary run %q", from, body.RootTaskID, f.root)
		}
		if len(body.Legs) != 4 {
			t.Fatalf("from %s: legs = %d (%+v), want all four", from, len(body.Legs), body.Legs)
		}
		got := map[string]string{}
		for _, leg := range body.Legs {
			got[leg.TaskID] = leg.LegRole
		}
		for id, role := range wantRoles {
			if got[id] != role {
				t.Errorf("from %s: leg %s role = %q, want %q", from, id, got[id], role)
			}
		}
		// $1.00 total: the point of the aggregate is that a review or a
		// fallback is spend the primary run's own usage never shows.
		if body.Totals.Legs != 4 || body.Totals.CostUsdTicks != 10_000_000_000 {
			t.Errorf("from %s: totals = %+v, want 4 legs / 10000000000 ticks", from, body.Totals)
		}
		if body.Totals.InputTokens != 400 || body.Totals.OutputTokens != 40 {
			t.Errorf("from %s: token totals = %+v, want 400 in / 40 out", from, body.Totals)
		}
	}
}

// A run that belongs to no workflow is a one-leg workflow rooted on itself,
// not an error and not an empty answer.
func TestTaskLegsSingleRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "task-legs-single-agent", nil)
	issueID := dbfx.Issue(t, "Task legs single issue", testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "completed"})

	body := fetchLegs(t, taskID)
	if body.RootTaskID != taskID || len(body.Legs) != 1 || body.Legs[0].LegRole != legRoleDraft {
		t.Fatalf("single run = %+v, want one draft leg rooted on itself", body)
	}
	if body.Totals.Legs != 1 || body.Totals.CostUsdTicks != 0 {
		t.Errorf("totals = %+v, want one leg and no recorded spend", body.Totals)
	}
}

// The access fence is the shared task loader's: a run of another workspace is
// not found, and a malformed id is a bad request rather than a panic.
func TestTaskLegsAccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	cases := []struct {
		name   string
		taskID string
		want   int
	}{
		{"malformed task id", "not-a-uuid", http.StatusBadRequest},
		{"unknown task", "00000000-0000-0000-0000-0000000000ff", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Call(t, testHandler.GetTaskLegs,
				testutil.WithURLParams(newRequest(http.MethodGet, "/api/tasks/"+tc.taskID+"/legs", nil), "taskId", tc.taskID)).
				Want(tc.want)
		})
	}
}

// TestTaskLegsKeepReviewOutOfRoutingStats: the routing statistics (JEF-237)
// score how a runtime performs on a task class. A review leg judges someone
// else's work, so counting it as a sample of the worker's class measures the
// wrong job; a retry is a real second attempt at the same class and must
// still count.
func TestTaskLegsKeepReviewOutOfRoutingStats(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "task-legs-routing-agent", nil)
	const class = "task-legs-routing-class"

	seedRoutingRun(t, agentID, runtimeID, "task-legs-retry-model", class,
		testutil.Cols{"leg_role": service.LegRoleRetry}, testutil.Cols{})
	seedRoutingRun(t, agentID, runtimeID, "task-legs-review-model", class,
		testutil.Cols{"leg_role": service.LegRoleReview}, testutil.Cols{})

	resp := testutil.Decode[RuntimeRoutingStatsResponse](t, testHandler.GetRuntimeRoutingStats,
		newRequest(http.MethodGet, "/api/runtimes/routing-stats", nil), http.StatusOK)

	if row := findRoutingStatsRow(resp, "task-legs-retry-model", class); row == nil || row.Samples != 1 {
		t.Errorf("retry leg = %+v, want one sample: a retry is another attempt at the class", row)
	}
	if row := findRoutingStatsRow(resp, "task-legs-review-model", class); row != nil {
		t.Errorf("review leg was counted as a worker sample: %+v", *row)
	}
}

// TestCrossReviewStampsReviewLeg: the cross-review producer (K15/JEF-238)
// stamps its run as a review leg of the run it reviews, so the reviewed run's
// workflow can be totalled and the review stays out of the routing samples.
func TestCrossReviewStampsReviewLeg(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	claude := providerRuntime(t, "claude")
	codex := providerRuntime(t, "codex")
	author := dbfx.Agent(t, "leg author", claude)
	reviewer := dbfx.Agent(t, "leg reviewer", codex)
	issue := dbfx.Issue(t, "Cross review leg issue", testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": author,
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, author, reviewer)
	})
	quietOtherAgents(t, author, reviewer)

	code := dbfx.Task(t, author, testutil.Cols{
		"runtime_id": claude, "issue_id": issue, "status": "completed",
		"completed_at": testutil.Raw("now()"),
	})
	review, err := testHandler.startCrossReview(context.Background(), mustTask(t, code), "branch feat/x", "")
	if err != nil {
		t.Fatalf("startCrossReview: %v", err)
	}
	stamped := mustTask(t, uuidToString(review.ID))
	if stamped.LegRole != service.LegRoleReview {
		t.Errorf("leg_role = %q, want %q", stamped.LegRole, service.LegRoleReview)
	}
	if uuidToString(stamped.WorkflowRootTaskID) != code {
		t.Errorf("workflow_root_task_id = %q, want the reviewed run %q", uuidToString(stamped.WorkflowRootTaskID), code)
	}
	// And the reviewed run's workflow now reports both legs.
	body := fetchLegs(t, code)
	if len(body.Legs) != 2 {
		t.Fatalf("legs = %+v, want the reviewed run and its review", body.Legs)
	}
}
