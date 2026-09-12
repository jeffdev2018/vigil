package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Dashboard vélocité mixte (JEF-251): throughput hebdo split member/agent,
// cycle time médian contre la période précédente, coût hebdo par issue
// fermée. Une issue fermée sans run compte dans le throughput et le cycle
// time mais est absente de la série de coût; une issue sans assignee_type est
// ignorée partout.

func callVelocityWeekly(t *testing.T, query string) DashboardVelocityWeeklyResponse {
	t.Helper()
	var out DashboardVelocityWeeklyResponse
	testutil.Call(t, testHandler.GetDashboardVelocityWeekly, newRequest(http.MethodGet, "/api/dashboard/velocity/weekly"+query, nil)).Want(http.StatusOK).JSON(&out)
	return out
}

// velocityIssue seeds a closed issue whose completed_at is `completedAgo` in
// the past and whose cycle time (completed_at - created_at) is exactly
// cycleDays.
func velocityIssue(t *testing.T, title string, assigneeType, assigneeID any, projectID, completedAgo string, cycleDays float64) string {
	t.Helper()
	id := dbfx.Issue(t, title, testutil.Cols{
		"status":        "done",
		"project_id":    projectID,
		"assignee_type": assigneeType,
		"assignee_id":   assigneeID,
	})
	dbfx.Exec(t, `UPDATE issue SET completed_at = now() - $2::interval WHERE id = $1`, id, completedAgo)
	dbfx.Exec(t, `UPDATE issue SET created_at = completed_at - ($2::float8 || ' days')::interval WHERE id = $1`, id, cycleDays)
	return id
}

func TestDashboardVelocityWeekly(t *testing.T) {
	agent := dbfx.Agent(t, "velocity agent", handlerTestRuntimeID(t))
	project := dbfx.Project(t, "velocity project")
	other := dbfx.Project(t, "velocity other project")

	// Current window, week of now()-3d: one member issue (2d cycle, 2e9 cost),
	// two agent issues with runs (0.5d/4e9 and 1.5d/no extra cost... A2 has a
	// run feeding nothing but its own issue cost) and one agent issue without
	// any run (1d cycle, throughput/cycle only).
	m1 := velocityIssue(t, "velocity m1", "member", testUserID, project, "3 days", 2)
	a1 := velocityIssue(t, "velocity a1", "agent", agent, project, "3 days", 0.5)
	a2 := velocityIssue(t, "velocity a2", "agent", agent, project, "3 days", 1.5)
	velocityIssue(t, "velocity a3 no runs", "agent", agent, project, "3 days", 1)
	// Same window, previous ISO week (exactly 7 days earlier): one member issue.
	m2 := velocityIssue(t, "velocity m2", "member", testUserID, project, "10 days", 4)
	// Unassigned: ignored by every series.
	velocityIssue(t, "velocity unassigned", nil, nil, project, "3 days", 100)
	// Previous window (now-60d..now-30d): feeds prev medians only.
	velocityIssue(t, "velocity prev member", "member", testUserID, project, "40 days", 6)
	velocityIssue(t, "velocity prev agent", "agent", agent, project, "40 days", 0.25)
	// Same shape in another project: invisible under the project filter.
	q1 := velocityIssue(t, "velocity other project issue", "agent", agent, other, "3 days", 3)

	costedRun(t, agent, m1, 2_000_000_000, false)
	costedRun(t, agent, a1, 1_000_000_000, false)
	costedRun(t, agent, a1, 3_000_000_000, false)
	costedRun(t, agent, a2, 6_000_000_000, false)
	costedRun(t, agent, m2, 800_000_000, false)
	costedRun(t, agent, q1, 9_000_000_000, false)

	var thisWeek, lastWeek string
	dbfx.QueryRow(t, `SELECT date_trunc('week', now() - interval '3 days')::date::text`).Scan(&thisWeek)
	dbfx.QueryRow(t, `SELECT date_trunc('week', now() - interval '10 days')::date::text`).Scan(&lastWeek)

	out := callVelocityWeekly(t, "?days=30&project_id="+project)

	// Throughput: two ISO weeks, ascending, member/agent split, unassigned
	// and previous-period issues excluded.
	if len(out.Throughput) != 2 {
		t.Fatalf("throughput = %+v, want two weeks", out.Throughput)
	}
	if out.Throughput[0].WeekStart != lastWeek || out.Throughput[0].MemberCount != 1 || out.Throughput[0].AgentCount != 0 {
		t.Fatalf("throughput[0] = %+v, want last week with one member issue", out.Throughput[0])
	}
	if out.Throughput[1].WeekStart != thisWeek || out.Throughput[1].MemberCount != 1 || out.Throughput[1].AgentCount != 3 {
		t.Fatalf("throughput[1] = %+v, want this week with 1 member + 3 agent issues", out.Throughput[1])
	}
	for _, wk := range out.Throughput {
		d, err := time.Parse("2006-01-02", wk.WeekStart)
		if err != nil || d.Weekday() != time.Monday {
			t.Fatalf("week_start %q is not an ISO Monday", wk.WeekStart)
		}
	}

	// Cycle time: member median of {2,4} = 3, agent median of {0.5,1,1.5} = 1;
	// previous window {6} and {0.25}.
	ct := out.CycleTime
	if ct.MemberCount != 2 || ct.AgentCount != 3 {
		t.Fatalf("cycle_time counts = %+v, want 2 member / 3 agent", ct)
	}
	if ct.MemberMedianDays == nil || *ct.MemberMedianDays != 3 || ct.AgentMedianDays == nil || *ct.AgentMedianDays != 1 {
		t.Fatalf("cycle_time medians = %+v, want member 3 / agent 1", ct)
	}
	if ct.PrevMemberMedianDays == nil || *ct.PrevMemberMedianDays != 6 || ct.PrevAgentMedianDays == nil || *ct.PrevAgentMedianDays != 0.25 {
		t.Fatalf("cycle_time prev medians = %+v, want member 6 / agent 0.25", ct)
	}

	// Cost per closed issue: week buckets sum each issue's runs; the no-run
	// issue is absent. This week: m1 2e9 + a1 4e9 + a2 6e9 = 12e9 over 3
	// issues; last week: m2 alone at 8e8.
	if len(out.CostPerClosedIssue) != 2 {
		t.Fatalf("cost_per_closed_issue = %+v, want two weeks", out.CostPerClosedIssue)
	}
	if wk := out.CostPerClosedIssue[0]; wk.WeekStart != lastWeek || wk.IssueCount != 1 || wk.TotalCostUsdTicks != 800_000_000 || wk.MeanCostUsdTicks != 800_000_000 {
		t.Fatalf("cost week last = %+v, want m2 alone at 8e8", wk)
	}
	if wk := out.CostPerClosedIssue[1]; wk.WeekStart != thisWeek || wk.IssueCount != 3 || wk.TotalCostUsdTicks != 12_000_000_000 || wk.MeanCostUsdTicks != 4_000_000_000 {
		t.Fatalf("cost week this = %+v, want 3 issues at 12e9 total / 4e9 mean", wk)
	}

	// The other project sees only its own issue.
	otherOut := callVelocityWeekly(t, "?days=30&project_id="+other)
	if len(otherOut.Throughput) != 1 || otherOut.Throughput[0].AgentCount != 1 || otherOut.Throughput[0].MemberCount != 0 {
		t.Fatalf("other project throughput = %+v", otherOut.Throughput)
	}
	if otherOut.CycleTime.AgentMedianDays == nil || *otherOut.CycleTime.AgentMedianDays != 3 || otherOut.CycleTime.MemberMedianDays != nil {
		t.Fatalf("other project cycle_time = %+v, want agent 3 and member null", otherOut.CycleTime)
	}
	if len(otherOut.CostPerClosedIssue) != 1 || otherOut.CostPerClosedIssue[0].TotalCostUsdTicks != 9_000_000_000 {
		t.Fatalf("other project costs = %+v", otherOut.CostPerClosedIssue)
	}

	// A window holding nothing: empty series and null medians, not zeros.
	empty := callVelocityWeekly(t, "?days=1&project_id="+project)
	if len(empty.Throughput) != 0 || len(empty.CostPerClosedIssue) != 0 {
		t.Fatalf("1d window = %+v, want empty series", empty)
	}
	ec := empty.CycleTime
	if ec.MemberMedianDays != nil || ec.AgentMedianDays != nil || ec.PrevMemberMedianDays != nil || ec.PrevAgentMedianDays != nil || ec.MemberCount != 0 || ec.AgentCount != 0 {
		t.Fatalf("1d cycle_time = %+v, want all null and zero counts", ec)
	}

	// A malformed project filter is a 400, like every dashboard endpoint.
	testutil.Call(t, testHandler.GetDashboardVelocityWeekly, newRequest(http.MethodGet, "/api/dashboard/velocity/weekly?days=30&project_id=not-a-uuid", nil)).Want(http.StatusBadRequest)
}
