package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Run cost honesty (audit UX, sept. 2026): the fleet list, its "cost today"
// tile, a workflow's leg totals and the pre-launch estimate price usage the
// way budget settlement does — the provider's reported cost, else the
// catalog estimate — and say "unknown" instead of rendering a zero for a run
// that recorded no priceable usage. Before, a Claude/Codex run that reported
// tokens but no provider cost read "$0.00" next to an issue page that said
// "cost unavailable" for the very same run.

// gpt-5.4 at 1M input + 1M output tokens is 175_000_000_000 ticks in the
// shared catalog (pkg/pricing TestEstimateTicksMatchesUIRules).
const pricedUsageTicks = 175_000_000_000

func TestRunsPriceUnreportedUsageAndFlagUnknownCost(t *testing.T) {
	agent := dbfx.Agent(t, "run cost agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), nil)
	issue := dbfx.Issue(t, "run cost issue "+uuid.NewString()[:8], testutil.Cols{"status": "in_progress"})
	cols := func() testutil.Cols {
		return testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed",
			"started_at": testutil.Raw("now() - interval '2 minutes'"), "completed_at": testutil.Raw("now()")}
	}
	estimated := dbfx.Task(t, agent, cols())
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": estimated, "provider": "openai", "model": "gpt-5.4", "input_tokens": 1_000_000, "output_tokens": 1_000_000})
	silent := dbfx.Task(t, agent, cols())

	feed := listRuns(t, "?issue_id="+issue)
	e := findRun(feed.Runs, estimated)
	if e == nil || e.CostUsdTicks != pricedUsageTicks || !e.CostKnown {
		t.Fatalf("estimated row = %+v, want %d ticks and a known cost", e, pricedUsageTicks)
	}
	s := findRun(feed.Runs, silent)
	if s == nil || s.CostUsdTicks != 0 || s.CostKnown {
		t.Fatalf("run without usage = %+v, want an unknown cost, not a free run", s)
	}
	if feed.Summary.CostSinceUsdTicks < pricedUsageTicks || feed.Summary.CostUnknownSince < 1 {
		t.Fatalf("summary = %+v, want the estimate counted and the silent run flagged", feed.Summary)
	}
}

func TestTaskLegsPriceUnreportedUsageAndFlagUnknownLegs(t *testing.T) {
	agent := dbfx.Agent(t, "leg cost agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), nil)
	issue := dbfx.Issue(t, "leg cost issue "+uuid.NewString()[:8], testutil.Cols{"status": "in_progress"})
	root := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed"})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": root, "provider": "openai", "model": "gpt-5.4", "input_tokens": 1_000_000, "output_tokens": 1_000_000})
	retry := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "failed", "leg_role": "retry", "workflow_root_task_id": root})

	body := fetchLegs(t, root)
	if body.Totals.Legs != 2 || body.Totals.CostUsdTicks != pricedUsageTicks || body.Totals.UnknownCostLegs != 1 {
		t.Fatalf("totals = %+v, want the estimate and one leg of unknown cost", body.Totals)
	}
	for _, leg := range body.Legs {
		if want := leg.TaskID != retry; leg.CostKnown != want {
			t.Errorf("leg %s cost_known = %v, want %v", leg.TaskID, leg.CostKnown, want)
		}
	}
}

func TestAgentCostEstimateAveragesPricedRecentRuns(t *testing.T) {
	agent := dbfx.Agent(t, "estimate agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), nil)
	get := func() AgentCostEstimateResponse {
		var out AgentCostEstimateResponse
		req := testutil.WithURLParams(newRequest(http.MethodGet, "/api/agents/"+agent+"/cost-estimate", nil), "id", agent)
		testutil.Call(t, testHandler.GetAgentCostEstimate, req).Want(http.StatusOK).JSON(&out)
		return out
	}
	if out := get(); out.AvgCostUsdTicks != nil || out.SampleRuns != 0 {
		t.Fatalf("no history = %+v, want no figure rather than a free agent", out)
	}
	priced := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "status": "completed", "completed_at": testutil.Raw("now()")})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": priced, "provider": "openai", "model": "gpt-5.4", "input_tokens": 1_000_000, "output_tokens": 1_000_000})
	reported := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "status": "completed", "completed_at": testutil.Raw("now()")})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": reported, "provider": "x", "model": "unpriced-model", "input_tokens": 10, "cost_usd_ticks": 25_000_000_000})
	unpriced := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "status": "completed", "completed_at": testutil.Raw("now()")})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": unpriced, "provider": "x", "model": "unpriced-model", "input_tokens": 10})

	out := get()
	if out.SampleRuns != 2 || out.AvgCostUsdTicks == nil || *out.AvgCostUsdTicks != (pricedUsageTicks+25_000_000_000)/2 {
		t.Fatalf("estimate = %+v, want the mean of the two priced runs", out)
	}
	bad := testutil.WithURLParams(newRequest(http.MethodGet, "/api/agents/nope/cost-estimate", nil), "id", "nope")
	testutil.Call(t, testHandler.GetAgentCostEstimate, bad).Want(http.StatusBadRequest)
}

// A failed run's replay names its failure reason, so mobile can say why in
// plain words before the audit payload (it has no other source for it).
func TestRunReplayCarriesFailureReason(t *testing.T) {
	agent := dbfx.Agent(t, "replay failure agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), nil)
	issue := dbfx.Issue(t, "replay failure issue "+uuid.NewString()[:8], testutil.Cols{"status": "todo"})
	failed := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "failed", "failure_reason": "runtime_offline"})
	var out struct {
		Run struct {
			FailureReason string `json:"failure_reason"`
		} `json:"run"`
	}
	testutil.Call(t, testHandler.GetTaskReplay, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/tasks/"+failed+"/replay", nil), "taskId", failed)).Want(http.StatusOK).JSON(&out)
	if out.Run.FailureReason != "runtime_offline" {
		t.Fatalf("replay failure_reason = %q, want runtime_offline", out.Run.FailureReason)
	}
}
