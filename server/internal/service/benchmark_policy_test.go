package service

import (
	"math"
	"testing"
)

// Offline policy hill-climbing (JEF-276). The search replays the router's own
// scoring over benchmark results for a grid of weights and reports — never
// applies — the policy that would have picked best.

func ptr(v float64) *float64 { return &v }

// resultsWhereCostDecides: two candidates that pass identically on the same
// class, one of them ten times cheaper. Only the cost weight can separate
// them, so the winner must be the cheap one under every grid point.
func resultsWhereCostDecides() []BenchmarkClassResult {
	return []BenchmarkClassResult{
		{RunID: "run-expensive", RuntimeID: "rt-a", Model: "big", TaskClass: "bugfix",
			Cases: 10, Passed: 9, AvgCostUSD: ptr(1.00), AvgDurationSecs: ptr(60)},
		{RunID: "run-cheap", RuntimeID: "rt-b", Model: "small", TaskClass: "bugfix",
			Cases: 10, Passed: 9, AvgCostUSD: ptr(0.10), AvgDurationSecs: ptr(60)},
	}
}

func TestBenchmarkPolicySearchScoresTheWholeGrid(t *testing.T) {
	out := BenchmarkPolicySearch(resultsWhereCostDecides())

	if len(out.Grid) != len(benchmarkCostWeights)*len(benchmarkDurationWeights)*len(benchmarkMinSamples) {
		t.Fatalf("the grid is the full cross product, got %d points", len(out.Grid))
	}
	baselines := 0
	for _, point := range out.Grid {
		if point.Baseline {
			baselines++
		}
	}
	if baselines != 1 || out.Baseline.Policy != CurrentBenchmarkPolicy() {
		t.Fatalf("exactly one point is the router's current policy: %d marked, baseline=%+v", baselines, out.Baseline.Policy)
	}

	// Every policy scores the class (10 cases clears every sample floor) and
	// picks the candidate that costs a tenth for the same success rate.
	for _, point := range out.Grid {
		if point.ScoredClasses != 1 || len(point.Picks) != 1 {
			t.Fatalf("policy %+v scored %d classes: %+v", point.Policy, point.ScoredClasses, point.Picks)
		}
		if point.Picks[0].RunID != "run-cheap" {
			t.Fatalf("policy %+v picked the expensive candidate at equal success: %+v", point.Policy, point.Picks[0])
		}
		if point.PassedRate != 0.9 || point.Cases != 10 || point.Passed != 9 {
			t.Fatalf("policy %+v aggregates its picks: %+v", point.Policy, point)
		}
	}
	// Nothing separates the points here, so the winner is the baseline itself
	// (first best in grid order) and the report says so.
	if out.Improved || out.Winner.Policy != CurrentBenchmarkPolicy() {
		t.Fatalf("nothing separates the policies, so the incumbent keeps it: winner=%+v", out.Winner.Policy)
	}
}

func TestBenchmarkPolicySearchSampleFloorGatesACandidate(t *testing.T) {
	// The strong candidate has only 4 measured cases: min_samples 3 scores it,
	// 5 and 10 do not — and with nobody else on the class, those policies
	// decide nothing rather than picking an unproven pair.
	out := BenchmarkPolicySearch([]BenchmarkClassResult{
		{RunID: "run-thin", RuntimeID: "rt-a", Model: "m", TaskClass: "feature",
			Cases: 4, Passed: 4, AvgCostUSD: ptr(0.5)},
	})
	for _, point := range out.Grid {
		want := 0
		if point.Policy.MinSamples <= 4 {
			want = 1
		}
		if point.ScoredClasses != want {
			t.Fatalf("min_samples=%d scored %d classes on 4 cases, want %d",
				point.Policy.MinSamples, point.ScoredClasses, want)
		}
	}
	// The winner is a policy that actually decided: passing 4/4 beats the
	// zero rate of the policies that scored nothing.
	if out.Winner.Policy.MinSamples != 3 || out.Winner.PassedRate != 1 {
		t.Fatalf("winner = %+v (rate %v)", out.Winner.Policy, out.Winner.PassedRate)
	}
	if !out.Improved {
		t.Fatalf("the current min_samples=5 scores nothing here, so 3 is an improvement: %+v", out.Baseline)
	}
	if out.Baseline.ScoredClasses != 0 || out.Baseline.AvgCostUSD != nil {
		t.Fatalf("the baseline decided nothing and has nothing to average: %+v", out.Baseline)
	}
}

func TestBenchmarkPolicySearchPrefersSuccessOverCost(t *testing.T) {
	// A cheap candidate that fails half the time must never outrank a reliable
	// one on price alone: the cost weight tops out at 0.5 while the Wilson gap
	// here is wider. Durations are held equal so cost is the only lever —
	// stacking a duration penalty on top CAN flip this, which is precisely the
	// kind of thing the search exists to surface.
	out := BenchmarkPolicySearch([]BenchmarkClassResult{
		{RunID: "run-reliable", RuntimeID: "rt-a", Model: "m", TaskClass: "docs",
			Cases: 20, Passed: 20, AvgCostUSD: ptr(2.0), AvgDurationSecs: ptr(60)},
		{RunID: "run-flaky", RuntimeID: "rt-b", Model: "m", TaskClass: "docs",
			Cases: 20, Passed: 10, AvgCostUSD: ptr(0.01), AvgDurationSecs: ptr(60)},
	})
	for _, point := range out.Grid {
		if point.Picks[0].RunID != "run-reliable" {
			t.Fatalf("policy %+v traded a 100%% success rate for a cheap coin flip: %+v", point.Policy, point.Picks[0])
		}
	}
}

func TestBenchmarkPolicySearchUnpricedCandidateIsNotFree(t *testing.T) {
	// rt-b reported no price at all. Scoring it as 0 would make silence the
	// cheapest possible bid; it takes the neutral midpoint instead, so the
	// candidate that is measurably cheaper than the pool's worst still wins.
	out := BenchmarkPolicySearch([]BenchmarkClassResult{
		{RunID: "run-priced", RuntimeID: "rt-a", Model: "m", TaskClass: "tests",
			Cases: 10, Passed: 8, AvgCostUSD: ptr(0.10)},
		{RunID: "run-unpriced", RuntimeID: "rt-b", Model: "m", TaskClass: "tests",
			Cases: 10, Passed: 8},
		{RunID: "run-dear", RuntimeID: "rt-c", Model: "m", TaskClass: "tests",
			Cases: 10, Passed: 8, AvgCostUSD: ptr(5.00)},
	})
	for _, point := range out.Grid {
		if point.Picks[0].RunID != "run-priced" {
			t.Fatalf("policy %+v let an unpriced candidate pass for free: %+v", point.Policy, point.Picks[0])
		}
		if point.AvgCostUSD == nil || math.Abs(*point.AvgCostUSD-0.10) > 1e-9 {
			t.Fatalf("the outcome averages the picks' real cost: %v", point.AvgCostUSD)
		}
	}
}

func TestBenchmarkPolicySearchIsDeterministicAndIgnoresUnmeasuredRows(t *testing.T) {
	results := []BenchmarkClassResult{
		{RunID: "run-a", RuntimeID: "rt-a", Model: "m", TaskClass: "chore", Cases: 6, Passed: 5, AvgCostUSD: ptr(0.2)},
		{RunID: "run-b", RuntimeID: "rt-b", Model: "m", TaskClass: "chore", Cases: 6, Passed: 5, AvgCostUSD: ptr(0.2)},
		// Nothing was measured here, so it cannot support a decision.
		{RunID: "run-empty", RuntimeID: "rt-c", Model: "m", TaskClass: "general", Cases: 0, Passed: 0},
	}
	first := BenchmarkPolicySearch(results)
	second := BenchmarkPolicySearch(results)
	if first.Winner.Policy != second.Winner.Policy || len(first.Grid) != len(second.Grid) {
		t.Fatalf("the search is a pure function: %+v vs %+v", first.Winner.Policy, second.Winner.Policy)
	}
	for _, point := range first.Grid {
		// 6 cases clear the 3 and 5 floors but not 10; the empty class never
		// counts as a decision under any of them.
		want := 0
		if point.Policy.MinSamples <= 6 {
			want = 1
		}
		if point.ScoredClasses != want {
			t.Fatalf("min_samples=%d scored %d classes, want %d: %+v", point.Policy.MinSamples, point.ScoredClasses, want, point)
		}
		// Two identical candidates: the tie keeps the first, always.
		if want == 1 && point.Picks[0].RunID != "run-a" {
			t.Fatalf("ties break on input order: %+v", point.Picks[0])
		}
	}
}

func TestBenchmarkPolicySearchWithNoResults(t *testing.T) {
	out := BenchmarkPolicySearch(nil)
	if len(out.Grid) == 0 || out.Winner.ScoredClasses != 0 || out.Improved ||
		out.Winner.Policy != CurrentBenchmarkPolicy() {
		t.Fatalf("an empty benchmark decides nothing and improves nothing: %+v", out.Winner)
	}
	if out.Baseline.Policy != CurrentBenchmarkPolicy() {
		t.Fatalf("baseline = %+v", out.Baseline.Policy)
	}
}
