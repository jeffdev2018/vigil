package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// ROI per agent (JEF-252): cost against closed issues, per agent, with the
// previous period alongside. An agent that spent without closing anything keeps
// its row with a null ratio and sorts last — that row is the point of the card.

func callAgentRoi(t *testing.T, query string) DashboardAgentRoiResponse {
	t.Helper()
	var out DashboardAgentRoiResponse
	testutil.Call(t, testHandler.GetDashboardAgentRoi, newRequest(http.MethodGet, "/api/dashboard/roi-by-agent"+query, nil)).Want(http.StatusOK).JSON(&out)
	return out
}

func roiRow(t *testing.T, out DashboardAgentRoiResponse, agentID string) AgentRoiRow {
	t.Helper()
	for _, row := range out.Agents {
		if row.AgentID == agentID {
			return row
		}
	}
	t.Fatalf("agent %s missing from %+v", agentID, out.Agents)
	return AgentRoiRow{}
}

func TestDashboardAgentRoiRanksAgentsByCostPerClosedIssue(t *testing.T) {
	runtime := handlerTestRuntimeID(t)
	cheap := dbfx.Agent(t, "roi cheap agent", runtime)
	burner := dbfx.Agent(t, "roi burner agent", runtime)
	project := dbfx.Project(t, "roi project")

	// Cheap agent: two issues closed this period, 300 + 500 ticks of runs.
	for _, cost := range []int64{300, 500} {
		issue := dbfx.Issue(t, "roi closed", testutil.Cols{
			"status": "done", "project_id": project,
			"assignee_type": "agent", "assignee_id": cheap,
		})
		costedRun(t, cheap, issue, cost, false)
	}
	// Burner: one run, no closed issue. Its issue is in the project so the
	// project filter keeps the run.
	open := dbfx.Issue(t, "roi still open", testutil.Cols{
		"status": "in_progress", "project_id": project,
		"assignee_type": "agent", "assignee_id": burner,
	})
	costedRun(t, burner, open, 1000, false)
	// Closed 40 days ago with a 2000-tick run: outside this window, inside the
	// previous one, so it only feeds the trend.
	previous := dbfx.Issue(t, "roi closed last period", testutil.Cols{
		"status": "done", "project_id": project,
		"assignee_type": "agent", "assignee_id": cheap,
	})
	prevTask := costedRun(t, cheap, previous, 2000, false)
	dbfx.Exec(t, `UPDATE issue SET completed_at = now() - interval '40 days' WHERE id = $1`, previous)
	dbfx.Exec(t, `UPDATE agent_task_queue SET completed_at = now() - interval '40 days' WHERE id = $1`, prevTask)

	out := callAgentRoi(t, "?days=30&project_id="+project)
	if out.Days != 30 || len(out.Agents) != 2 {
		t.Fatalf("response = %+v, want the two agents with data", out)
	}
	if out.Agents[0].AgentID != cheap || out.Agents[1].AgentID != burner {
		t.Fatalf("order = %s, %s; want the agent with a ratio ahead of the one without", out.Agents[0].AgentID, out.Agents[1].AgentID)
	}

	a := roiRow(t, out, cheap)
	if a.IssuesClosed != 2 || a.CostUSDTicks != 800 || a.AgentName != "roi cheap agent" {
		t.Fatalf("cheap agent = %+v, want 2 issues at 800 ticks; the 40-day-old issue must be out of the window", a)
	}
	if a.CostPerIssueUSDTicks == nil || *a.CostPerIssueUSDTicks != 400 {
		t.Fatalf("cheap cost per issue = %v, want 400", a.CostPerIssueUSDTicks)
	}
	if a.PrevCostPerIssueUSDTicks == nil || *a.PrevCostPerIssueUSDTicks != 2000 {
		t.Fatalf("cheap previous cost per issue = %v, want 2000 from the 40-day-old close", a.PrevCostPerIssueUSDTicks)
	}
	if a.CostPerPRUSDTicks != nil || a.PRsMerged != 0 {
		t.Fatalf("no PR was merged, so there is no cost per PR: %+v", a)
	}

	b := roiRow(t, out, burner)
	if b.IssuesClosed != 0 || b.CostUSDTicks != 1000 {
		t.Fatalf("burner = %+v, want 1000 ticks and nothing closed", b)
	}
	if b.CostPerIssueUSDTicks != nil || b.PrevCostPerIssueUSDTicks != nil {
		t.Fatalf("closing nothing means no cost per issue, not zero: %+v", b)
	}

	// Another project sees neither agent.
	if out := callAgentRoi(t, "?days=30&project_id="+dbfx.Project(t, "roi other project")); len(out.Agents) != 0 {
		t.Fatalf("other project = %+v, want no rows", out.Agents)
	}
	// A project_id that is not a UUID is rejected, not silently ignored.
	testutil.Call(t, testHandler.GetDashboardAgentRoi, newRequest(http.MethodGet, "/api/dashboard/roi-by-agent?days=30&project_id=not-a-uuid", nil)).Want(http.StatusBadRequest)
}

func TestDashboardAgentRoiFoldsRestrictedAgentsIntoOneBucket(t *testing.T) {
	rows := foldRestrictedAgentRoi(
		[]AgentRoiRow{
			{AgentID: "visible", AgentName: "Visible", IssuesClosed: 1, CostUSDTicks: 100},
			{AgentID: "hidden-a", AgentName: "Private A", Provider: "anthropic", IssuesClosed: 2, PRsMerged: 1, CostUSDTicks: 400, UncostedRuns: 1},
			{AgentID: "hidden-b", AgentName: "Private B", Provider: "openai", IssuesClosed: 2, CostUSDTicks: 200},
		},
		map[string]struct{}{"hidden-a": {}, "hidden-b": {}},
	)
	if len(rows) != 2 || rows[0].AgentID != "visible" {
		t.Fatalf("rows = %+v, want the visible agent plus one bucket", rows)
	}
	bucket := rows[1]
	want := AgentRoiRow{AgentID: restrictedAgentsRowID, IssuesClosed: 4, PRsMerged: 1, CostUSDTicks: 600, UncostedRuns: 1}
	if bucket != want {
		t.Fatalf("bucket = %+v, want %+v with no name and no provider", bucket, want)
	}
	// The bucket's ratio is its merged cost over its merged count, never a sum
	// of the ratios it swallowed.
	if got := costPer(bucket.CostUSDTicks, bucket.IssuesClosed); got == nil || *got != 150 {
		t.Fatalf("bucket cost per issue = %v, want 150", got)
	}
	if costPer(500, 0) != nil {
		t.Fatal("no deliverable must mean no ratio, not a division by zero")
	}
}
