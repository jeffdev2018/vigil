package service

import (
	"math"
	"sort"
)

// Offline policy hill-climbing (JEF-276).
//
// A benchmark replays one suite against several (runtime, model) candidates,
// so it produces exactly the shape the runtime router scores on: per class,
// per candidate, a success count over a known number of attempts, with the
// cost and duration those attempts spent. That makes it possible to ask a
// question the live router cannot answer about itself — "would another set of
// weights have picked better candidates on this evidence?" — by replaying its
// own scoring over a small grid of weights.
//
// Everything here is a pure function of the benchmark results. No LLM, no
// database, no randomness: the exploration draw is deliberately left out
// because it is what a policy does to LEARN, and this replays what a policy
// would have DECIDED on evidence already gathered. The result is reported,
// never applied: changing the router's constants is a human decision.

// Grid searched by BenchmarkPolicySearch. Small on purpose — 27 points, each
// a plausible operating point rather than a sweep, so the answer stays
// readable and the search stays instant.
var (
	benchmarkCostWeights     = []float64{0.1, 0.3, 0.5}
	benchmarkDurationWeights = []float64{0, 0.1, 0.3}
	benchmarkMinSamples      = []int{3, 5, 10}
)

// BenchmarkClassResult is one candidate's record on one task class, as
// measured by a benchmark run. Cases is the number of replays that produced a
// verdict and Passed how many of them proved every criterion.
type BenchmarkClassResult struct {
	RunID     string
	RuntimeID string
	Model     string
	TaskClass string
	Cases     int
	Passed    int
	// AvgCostUSD / AvgDurationSecs are nil when nothing in the bucket
	// reported a price or ever started — which is not the same as free or
	// instantaneous, and must not be scored as if it were.
	AvgCostUSD      *float64
	AvgDurationSecs *float64
}

// BenchmarkPolicy is one point of the grid: the router's three tunable
// constants.
type BenchmarkPolicy struct {
	CostWeight     float64 `json:"cost_weight"`
	DurationWeight float64 `json:"duration_weight"`
	MinSamples     int     `json:"min_samples"`
}

// BenchmarkPolicyPick is the candidate a policy would route one task class to.
type BenchmarkPolicyPick struct {
	TaskClass  string   `json:"task_class"`
	RunID      string   `json:"run_id"`
	RuntimeID  string   `json:"runtime_id"`
	Model      string   `json:"model"`
	Score      float64  `json:"score"`
	Cases      int      `json:"cases"`
	Passed     int      `json:"passed"`
	AvgCostUSD *float64 `json:"avg_cost_usd"`
}

// BenchmarkPolicyOutcome is what one policy would have achieved over the whole
// benchmark: the candidate it picks per class, and what those picks passed and
// cost in aggregate.
type BenchmarkPolicyOutcome struct {
	Policy BenchmarkPolicy `json:"policy"`
	// Baseline marks the point that matches the router's current constants,
	// so the reader can see what is being improved on.
	Baseline bool `json:"baseline"`
	// ScoredClasses counts the classes where at least one candidate cleared
	// the sample floor. A policy that scores nothing decided nothing.
	ScoredClasses int                   `json:"scored_classes"`
	Cases         int                   `json:"cases"`
	Passed        int                   `json:"passed"`
	PassedRate    float64               `json:"passed_rate"`
	AvgCostUSD    *float64              `json:"avg_cost_usd"`
	Picks         []BenchmarkPolicyPick `json:"picks"`
}

// BenchmarkPolicySearchResult is the whole grid plus the two points worth
// naming.
type BenchmarkPolicySearchResult struct {
	Grid     []BenchmarkPolicyOutcome `json:"grid"`
	Baseline BenchmarkPolicyOutcome   `json:"baseline"`
	Winner   BenchmarkPolicyOutcome   `json:"winner"`
	// Improved is true when the winner is a different policy than the
	// baseline. It is the whole point of the report: a false here says the
	// current constants already do the best this evidence can show.
	Improved bool `json:"improved"`
}

// CurrentBenchmarkPolicy is the router's live configuration, used as the
// baseline every grid point is compared against.
func CurrentBenchmarkPolicy() BenchmarkPolicy {
	return BenchmarkPolicy{
		CostWeight:     routingCostWeight,
		DurationWeight: routingDurationWeight,
		MinSamples:     routingMinScoredSamples,
	}
}

// BenchmarkPolicySearch replays the router's scoring over the grid and reports
// the policy that maximises passed rate at the lowest cost. Pure: same input,
// same output, always.
func BenchmarkPolicySearch(results []BenchmarkClassResult) BenchmarkPolicySearchResult {
	byClass := map[string][]BenchmarkClassResult{}
	classes := []string{}
	for _, r := range results {
		if r.Cases <= 0 {
			continue // nothing was measured; it cannot support a decision
		}
		if _, seen := byClass[r.TaskClass]; !seen {
			classes = append(classes, r.TaskClass)
		}
		byClass[r.TaskClass] = append(byClass[r.TaskClass], r)
	}
	sort.Strings(classes)

	current := CurrentBenchmarkPolicy()
	out := BenchmarkPolicySearchResult{Grid: []BenchmarkPolicyOutcome{}}
	for _, cost := range benchmarkCostWeights {
		for _, duration := range benchmarkDurationWeights {
			for _, minSamples := range benchmarkMinSamples {
				policy := BenchmarkPolicy{CostWeight: cost, DurationWeight: duration, MinSamples: minSamples}
				outcome := evaluateBenchmarkPolicy(policy, classes, byClass)
				outcome.Baseline = policy == current
				if outcome.Baseline {
					out.Baseline = outcome
				}
				out.Grid = append(out.Grid, outcome)
			}
		}
	}
	// The search starts from the baseline, so a grid point only becomes the
	// winner by being STRICTLY better than the router's current constants.
	// Anything else would report an arbitrary tie as an improvement and send
	// someone to change a working policy for nothing.
	out.Winner = pickBenchmarkWinner(out.Baseline, out.Grid)
	out.Improved = out.Winner.Policy != out.Baseline.Policy
	return out
}

// evaluateBenchmarkPolicy picks the best candidate per class under one policy
// and aggregates what those picks achieved.
func evaluateBenchmarkPolicy(policy BenchmarkPolicy, classes []string, byClass map[string][]BenchmarkClassResult) BenchmarkPolicyOutcome {
	outcome := BenchmarkPolicyOutcome{Policy: policy, Picks: []BenchmarkPolicyPick{}}
	costWeighted, costCases := 0.0, 0
	for _, class := range classes {
		pick, ok := bestBenchmarkCandidate(policy, byClass[class])
		if !ok {
			continue
		}
		outcome.ScoredClasses++
		outcome.Cases += pick.Cases
		outcome.Passed += pick.Passed
		if pick.AvgCostUSD != nil {
			costWeighted += *pick.AvgCostUSD * float64(pick.Cases)
			costCases += pick.Cases
		}
		outcome.Picks = append(outcome.Picks, pick)
	}
	if outcome.Cases > 0 {
		outcome.PassedRate = float64(outcome.Passed) / float64(outcome.Cases)
	}
	if costCases > 0 {
		avg := costWeighted / float64(costCases)
		outcome.AvgCostUSD = &avg
	}
	return outcome
}

// bestBenchmarkCandidate scores one class's candidates exactly the way
// scoreRoutingCandidates does — Wilson lower bound minus min-max normalized
// cost and duration penalties — under the given policy's weights and sample
// floor. Candidates below the floor are not scored at all, which is the live
// router's rule: an unproven pair is explored, never exploited.
func bestBenchmarkCandidate(policy BenchmarkPolicy, candidates []BenchmarkClassResult) (BenchmarkPolicyPick, bool) {
	pool := make([]BenchmarkClassResult, 0, len(candidates))
	for _, c := range candidates {
		if c.Cases >= policy.MinSamples {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		return BenchmarkPolicyPick{}, false
	}
	normCost := normalizeBenchmarkMetric(pool, func(c BenchmarkClassResult) *float64 { return c.AvgCostUSD })
	normDuration := normalizeBenchmarkMetric(pool, func(c BenchmarkClassResult) *float64 { return c.AvgDurationSecs })

	best, bestScore := -1, math.Inf(-1)
	for i, c := range pool {
		score := wilsonLowerBound(c.Passed, c.Cases) -
			policy.CostWeight*normCost[i] -
			policy.DurationWeight*normDuration[i]
		// Strictly greater: ties keep the earlier candidate, so the same
		// input always yields the same pick.
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	winner := pool[best]
	return BenchmarkPolicyPick{
		TaskClass:  winner.TaskClass,
		RunID:      winner.RunID,
		RuntimeID:  winner.RuntimeID,
		Model:      winner.Model,
		Score:      bestScore,
		Cases:      winner.Cases,
		Passed:     winner.Passed,
		AvgCostUSD: winner.AvgCostUSD,
	}, true
}

// normalizeBenchmarkMetric min-max normalizes one metric over the pool.
// Members without data take the neutral midpoint, so "nobody reported a price"
// never reads as "free" — the same rule the live router applies.
func normalizeBenchmarkMetric(pool []BenchmarkClassResult, value func(BenchmarkClassResult) *float64) []float64 {
	out := make([]float64, len(pool))
	min, max := math.Inf(1), math.Inf(-1)
	anyData := false
	for _, c := range pool {
		if v := value(c); v != nil {
			anyData = true
			min = math.Min(min, *v)
			max = math.Max(max, *v)
		}
	}
	for i, c := range pool {
		v := value(c)
		switch {
		case v == nil || !anyData:
			out[i] = routingUnknownNormMidpoint
		case max == min:
			out[i] = 0
		default:
			out[i] = (*v - min) / (max - min)
		}
	}
	return out
}

// pickBenchmarkWinner is the report's headline: the policy whose picks pass
// the most, breaking ties on the cheaper one, then on the one that decided
// more classes. Ties keep the incumbent, so the search never recommends a
// change that buys nothing.
func pickBenchmarkWinner(baseline BenchmarkPolicyOutcome, grid []BenchmarkPolicyOutcome) BenchmarkPolicyOutcome {
	best := baseline
	for _, candidate := range grid {
		if betterBenchmarkOutcome(candidate, best) {
			best = candidate
		}
	}
	return best
}

func betterBenchmarkOutcome(a, b BenchmarkPolicyOutcome) bool {
	if a.PassedRate != b.PassedRate {
		return a.PassedRate > b.PassedRate
	}
	// A policy with no cost data does not get to claim it is the cheap one.
	switch {
	case a.AvgCostUSD != nil && b.AvgCostUSD != nil && *a.AvgCostUSD != *b.AvgCostUSD:
		return *a.AvgCostUSD < *b.AvgCostUSD
	case a.AvgCostUSD != nil && b.AvgCostUSD == nil:
		return true
	case a.AvgCostUSD == nil && b.AvgCostUSD != nil:
		return false
	}
	return a.ScoredClasses > b.ScoredClasses
}
