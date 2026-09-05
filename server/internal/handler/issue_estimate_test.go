package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// What-if estimate (K44): the comparable set is the agent's own completed
// runs on issues the competency audit trail (K43) put in the same domain;
// another domain's run must not count; below min_sample the numbers are
// withheld rather than guessed; a median that would push a budget past its
// limit is flagged.

type estimateResponse struct {
	DomainKey  string              `json:"domain_key"`
	MinSample  int                 `json:"min_sample"`
	Candidates []EstimateCandidate `json:"candidates"`
}

func TestEstimateFromSamples(t *testing.T) {
	samples := func(durations ...int64) []runSample {
		out := make([]runSample, len(durations))
		for i, d := range durations {
			out[i] = runSample{DurationSeconds: d, CostTicks: d * 10}
		}
		return out
	}
	// Six runs: the median interpolates between the two middle values, and
	// the range is the interquartile one.
	s := estimateFromSamples(samples(60, 120, 180, 240, 300, 360), 5)
	if s.SampleSize != 6 || s.InsufficientHistory {
		t.Fatalf("six runs over a threshold of five = %+v", s)
	}
	if *s.MedianDuration != 210 || *s.DurationRangeLow != 135 || *s.DurationRangeHigh != 285 {
		t.Fatalf("even sample duration = %d [%d, %d]", *s.MedianDuration, *s.DurationRangeLow, *s.DurationRangeHigh)
	}
	if *s.MedianCostTicks != 2100 || *s.CostRangeLowTicks != 1350 || *s.CostRangeHighTicks != 2850 {
		t.Fatalf("even sample cost = %d [%d, %d]", *s.MedianCostTicks, *s.CostRangeLowTicks, *s.CostRangeHighTicks)
	}
	// Odd sample: the median is a real observation.
	if s = estimateFromSamples(samples(10, 20, 30), 3); *s.MedianDuration != 20 || *s.DurationRangeLow != 15 || *s.DurationRangeHigh != 25 {
		t.Fatalf("odd sample = %+v", s)
	}
	// Order must not matter.
	if unsorted := estimateFromSamples(samples(30, 10, 20), 3); *unsorted.MedianDuration != 20 {
		t.Fatalf("unsorted median = %d", *unsorted.MedianDuration)
	}
	// A single run is still a median once the threshold allows it.
	if s = estimateFromSamples(samples(42), 1); *s.MedianDuration != 42 || *s.DurationRangeLow != 42 || *s.DurationRangeHigh != 42 {
		t.Fatalf("single sample = %+v", s)
	}
	// Below the threshold every number is withheld, not guessed.
	s = estimateFromSamples(samples(10, 20), 5)
	if s.SampleSize != 2 || !s.InsufficientHistory {
		t.Fatalf("two runs under a threshold of five = %+v", s)
	}
	if s.MedianDuration != nil || s.MedianCostTicks != nil || s.CostRangeLowTicks != nil || s.DurationRangeHigh != nil {
		t.Fatalf("an insufficient sample must carry no numbers: %+v", s)
	}
	if empty := estimateFromSamples(nil, 5); empty.SampleSize != 0 || !empty.InsufficientHistory || empty.MedianCostTicks != nil {
		t.Fatalf("no runs = %+v", empty)
	}
	// A nonsensical threshold must not turn an empty history into a number.
	if empty := estimateFromSamples(nil, 0); !empty.InsufficientHistory {
		t.Fatal("an empty history must stay insufficient whatever the threshold")
	}
}

func TestIssueEstimateEndpoint(t *testing.T) {
	rememberSettings(t)
	runtimeID := handlerTestRuntimeID(t)
	agentA := dbfx.Agent(t, "estimate agent a", runtimeID)
	agentB := dbfx.Agent(t, "estimate agent b", runtimeID)
	ws := parseUUID(testWorkspaceID)

	// One comparable run: an issue the audit trail places in domain, and a
	// completed run of agent on it that took seconds and cost ticks.
	run := func(agent, domain string, seconds, ticks int64) {
		t.Helper()
		issue := dbfx.Issue(t, fmt.Sprintf("Comparable %s %ds", domain, seconds))
		task := dbfx.Task(t, agent, testutil.Cols{
			"issue_id":     issue,
			"status":       "completed",
			"started_at":   testutil.Raw(fmt.Sprintf("now() - interval '%d seconds'", seconds)),
			"completed_at": testutil.Raw("now()"),
		})
		dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": task, "provider": "test", "model": "test-model", "cost_usd_ticks": ticks})
		testHandler.audit(t.Context(), ws, "system", "", AuditCompetency, "agent", parseUUID(agent),
			map[string]any{"issue_id": issue, "domain_key": domain, "event": "accepted"}, nil)
	}

	const tick = int64(1_000_000_000) // $0.10
	for i := int64(1); i <= 6; i++ {
		run(agentA, "path:server", i*60, i*tick)
	}
	run(agentB, "path:server", 999, 99*tick)
	run(agentB, "path:server", 999, 99*tick)
	// Another domain's run must not move agent A's numbers.
	run(agentA, "path:packages", 100_000, 500*tick)

	issue := dbfx.Issue(t, "Fix server/internal/handler/issue.go and server/cmd/main.go")
	get := func(query string, want int) estimateResponse {
		t.Helper()
		var out estimateResponse
		path := "/api/issues/" + issue + "/estimate" + query
		res := testutil.Call(t, testHandler.GetIssueEstimate, withURLParam(newRequest(http.MethodGet, path, nil), "id", issue)).Want(want)
		if want == http.StatusOK {
			res.JSON(&out)
		}
		return out
	}
	byAgent := func(r estimateResponse, id string) EstimateCandidate {
		t.Helper()
		for _, c := range r.Candidates {
			if c.AgentID == id {
				return c
			}
		}
		t.Fatalf("agent %s missing from %+v", id, r.Candidates)
		return EstimateCandidate{}
	}

	out := get("?candidates="+agentA+","+agentB, http.StatusOK)
	if out.DomainKey != "path:server" || out.MinSample != competencyDefaultMinSample || len(out.Candidates) != 2 {
		t.Fatalf("estimate envelope = %+v", out)
	}
	a := byAgent(out, agentA)
	if a.SampleSize != 6 || a.InsufficientHistory || a.AgentName != "estimate agent a" {
		t.Fatalf("agent A must have six comparable runs, got %+v", a)
	}
	if *a.MedianDuration != 210 || *a.DurationRangeLow != 135 || *a.DurationRangeHigh != 285 {
		t.Fatalf("agent A duration = %d [%d, %d]; the other domain's 100000s run must not count",
			*a.MedianDuration, *a.DurationRangeLow, *a.DurationRangeHigh)
	}
	if *a.MedianCostTicks != 3500000000 || *a.CostRangeLowTicks != 2250000000 || *a.CostRangeHighTicks != 4750000000 {
		t.Fatalf("agent A cost = %d [%d, %d]", *a.MedianCostTicks, *a.CostRangeLowTicks, *a.CostRangeHighTicks)
	}
	if a.ExceedsBudget {
		t.Fatal("no budget policy exists yet")
	}
	b := byAgent(out, agentB)
	if b.SampleSize != 2 || !b.InsufficientHistory || b.MedianCostTicks != nil || b.MedianDuration != nil {
		t.Fatalf("agent B has two runs under the threshold of five: %+v", b)
	}

	// Without candidates the agents with a competency row in this domain answer.
	dbfx.Cleanup(t, `DELETE FROM agent_domain_competency WHERE agent_id = ANY($1::uuid[])`, []string{agentA, agentB})
	testHandler.bumpCompetency(t.Context(), ws, parseUUID(agentA), "path:server", 1, 1, 0, 0)
	out = get("", http.StatusOK)
	if len(out.Candidates) != 1 || out.Candidates[0].AgentID != agentA || out.Candidates[0].SampleSize != 6 {
		t.Fatalf("implicit candidates = %+v", out.Candidates)
	}

	get("?candidates=not-a-uuid", http.StatusBadRequest)
	other := dbfx.Workspace(t, "estimate other ws", fmt.Sprintf("estimate-other-%s", agentA[:8]))
	foreign := dbfx.Agent(t, "estimate foreign agent", runtimeID, testutil.Cols{"workspace_id": other, "owner_id": testUserID})
	get("?candidates="+foreign, http.StatusBadRequest)

	// A limit below the median run marks the candidate over budget.
	dbfx.Insert(t, "budget_policy", testutil.Cols{
		"workspace_id": testWorkspaceID, "scope_type": "workspace", "limit_usd_ticks": tick,
		"period": "monthly", "action": "enforce", "created_by": testUserID,
	})
	out = get("?candidates="+agentA+","+agentB, http.StatusOK)
	if !byAgent(out, agentA).ExceedsBudget {
		t.Fatal("a median of $0.35 against a $0.10 limit must be over budget")
	}
	if byAgent(out, agentB).ExceedsBudget {
		t.Fatal("a candidate with no estimate must not be declared over budget")
	}
}
