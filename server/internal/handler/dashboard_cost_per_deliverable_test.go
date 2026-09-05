package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Cost per deliverable (K04): closed issues and merged PRs with runs, mean
// and median from task_usage, trend against the previous period; a manual
// close without a run is absent, not free.

func callCostPerDeliverable(t *testing.T, query string) CostPerDeliverableResponse {
	t.Helper()
	var out CostPerDeliverableResponse
	testutil.Call(t, testHandler.GetDashboardCostPerDeliverable, newRequest(http.MethodGet, "/api/dashboard/cost-per-deliverable"+query, nil)).Want(http.StatusOK).JSON(&out)
	return out
}

func costedRun(t *testing.T, agentID, issueID string, costTicks int64, extraUncosted bool) string {
	t.Helper()
	task := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t), "issue_id": issueID, "status": "completed",
		"started_at": testutil.Raw("now()"), "completed_at": testutil.Raw("now()"),
	})
	dbfx.Exec(t, `INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cost_usd_ticks, created_at) VALUES ($1, 'openai', 'priced', 10, 1, $2, now())`, task, costTicks)
	if extraUncosted {
		dbfx.Exec(t, `INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at) VALUES ($1, 'openai', 'unpriced', 10, 1, now())`, task)
	}
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM task_usage WHERE task_id = $1`, task) })
	return task
}

func TestCostPerDeliverableCountsOnlyDeliveredWorkWithRuns(t *testing.T) {
	agent := dbfx.Agent(t, "deliverable agent", handlerTestRuntimeID(t))
	project := dbfx.Project(t, "deliverable project")
	other := dbfx.Project(t, "other project")

	// Closed this period with two runs: 1e9 + 3e9 ticks, one unpriced row.
	done := dbfx.Issue(t, "done with runs", testutil.Cols{"status": "done", "project_id": project})
	costedRun(t, agent, done, 1_000_000_000, false)
	costedRun(t, agent, done, 3_000_000_000, true)
	var completedAt *string
	dbfx.QueryRow(t, `SELECT completed_at::text FROM issue WHERE id = $1`, done).Scan(&completedAt)
	if completedAt == nil {
		t.Fatal("entering done must stamp completed_at")
	}
	// Closed by hand, no run: absent from the metric.
	dbfx.Issue(t, "done without runs", testutil.Cols{"status": "done", "project_id": project})
	// Still open with runs: absent.
	open := dbfx.Issue(t, "open with runs", testutil.Cols{"status": "in_progress", "project_id": project})
	costedRun(t, agent, open, 9_000_000_000, false)
	// Closed in the previous period: feeds the trend only.
	previous := dbfx.Issue(t, "done last period", testutil.Cols{"status": "done", "project_id": project})
	costedRun(t, agent, previous, 8_000_000_000, false)
	dbfx.Exec(t, `UPDATE issue SET completed_at = now() - interval '40 days' WHERE id = $1`, previous)

	// One merged PR on the closed issue, one still open.
	conn := vcsConnection(t)
	merged := vcsPR(t, conn, done, 5)
	dbfx.Exec(t, `UPDATE vcs_pull_request SET state = 'merged', merged_at = now() WHERE id = $1`, merged)
	vcsPR(t, conn, done, 6)

	out := callCostPerDeliverable(t, "?days=30&project_id="+project)
	if out.Days != 30 || out.Issues.Count != 1 || out.Issues.MeanUsdTicks != 4_000_000_000 || out.Issues.MedianUsdTicks != 4_000_000_000 || out.Issues.UncostedCount != 1 {
		t.Fatalf("issues = %+v, want one closed issue at 4e9 ticks with an uncosted floor", out.Issues)
	}
	if out.Issues.TrendPct == nil || *out.Issues.TrendPct != -50 {
		t.Fatalf("issue trend = %v, want -50%% against the 8e9 previous period", out.Issues.TrendPct)
	}
	if out.PullRequests.Count != 1 || out.PullRequests.MeanUsdTicks != 4_000_000_000 || out.PullRequests.TrendPct != nil {
		t.Fatalf("prs = %+v, want the merged PR alone, no previous period to compare", out.PullRequests)
	}

	// Leaving done clears the stamp, so the issue drops out.
	dbfx.Exec(t, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, done)
	if out := callCostPerDeliverable(t, "?days=30&project_id="+project); out.Issues.Count != 0 {
		t.Fatalf("issues after reopening = %+v, want none", out.Issues)
	}
	// Another project sees nothing.
	if out := callCostPerDeliverable(t, "?days=30&project_id="+other); out.Issues.Count != 0 || out.PullRequests.Count != 0 {
		t.Fatalf("other project = %+v", out)
	}
	// Stats helpers: even count medians and a missing previous period.
	s := deliverableStats([]deliverableCost{{cost: 1}, {cost: 2}, {cost: 10}, {cost: 20, uncosted: true}})
	if s.MedianUsdTicks != 6 || s.MeanUsdTicks != 8 || s.UncostedCount != 1 || s.TotalUsdTicks != 33 {
		t.Fatalf("stats = %+v", s)
	}
	if withTrend(s, DeliverableCostStats{}).TrendPct != nil {
		t.Fatal("no previous period must mean no trend")
	}
}
