package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Runtime routing modes (agent.runtime_routing, migration 573, JEF-237).
const (
	// RoutingModeFixed is the default: the agent always runs on its bound
	// runtime with its configured model. Behavior is exactly the pre-JEF-237
	// enqueue path.
	RoutingModeFixed = "fixed"
	// RoutingModeAuto lets the server pick the most promising
	// (runtime, model) pair per enqueued task from historical run statistics.
	// The bound runtime_id / model stay in place as the routing fallback.
	RoutingModeAuto = "auto"
)

// Runtime router tuning knobs. All deterministic — no LLM is ever consulted.
const (
	// wilsonZ95 is the z-score for the 95% Wilson lower confidence bound used
	// as the pessimistic success estimate. 1 success out of 1 sample must
	// never outrank 190 out of 200: wilsonLower(1,1) ~= 0.21 <
	// wilsonLower(190,200) ~= 0.89.
	wilsonZ95 = 1.96

	// routingMinScoredSamples is the sample floor for entering the scored
	// pool. Below it a candidate can only be picked by the exploration draw.
	routingMinScoredSamples = 5
	// routingExcludeMinSamples + routingExcludeMaxSuccessRate implement the
	// floor guard: once a candidate has enough history to be trusted and that
	// history is bad, it is excluded outright (excluded_reason in the trace).
	routingExcludeMinSamples     = 10
	routingExcludeMaxSuccessRate = 0.50

	// routingExplorationEpsilon is the probability of skipping the best-scored
	// candidate in favor of the least-sampled under-sampled candidate, so new
	// (runtime, model) pairs can accrue the samples needed to become scored.
	routingExplorationEpsilon = 0.10

	// Score = wilson_lower − costWeight·normCost − durationWeight·normDuration.
	// Success dominates; cost is a secondary tiebreaker and duration a
	// tertiary one. Costs/durations are min-max normalized across the scored
	// pool so the weights stay comparable regardless of absolute magnitudes.
	// Candidates without cost/duration data take the neutral midpoint 0.5, so
	// "provider never reported a price" does not masquerade as "free".
	routingCostWeight          = 0.3
	routingDurationWeight      = 0.1
	routingUnknownNormMidpoint = 0.5

	// routingStatsWindow bounds how far back the statistics look.
	routingStatsWindow = 90 * 24 * time.Hour

	// costTicksPerUSD converts task_usage.cost_usd_ticks (1e-10 USD) to USD.
	costTicksPerUSD = 1e-10
)

// Routing decision reasons, recorded in the task's routing audit trace.
const (
	routingReasonBestScore          = "best_score"
	routingReasonExploreLeastSample = "explore_least_sampled"
	routingReasonNoCandidates       = "fallback_no_candidate_runtimes"
	routingReasonQueryFailed        = "fallback_query_failed"
	routingReasonNoScoredCandidates = "fallback_no_scored_candidates"
)

// RoutingCandidateTrace is the per-candidate audit record embedded in the
// task's routing JSONB column.
type RoutingCandidateTrace struct {
	RuntimeID       string   `json:"runtime_id"`
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	Samples         int      `json:"samples"`
	SuccessRate     float64  `json:"success_rate"`
	WilsonLower     float64  `json:"wilson_lower"`
	AvgCostUSD      *float64 `json:"avg_cost_usd,omitempty"`
	AvgDurationSecs *float64 `json:"avg_duration_secs,omitempty"`
	Score           *float64 `json:"score,omitempty"`
	ExcludedReason  string   `json:"excluded_reason,omitempty"`
}

// RuntimeRoutingDecision is the full audit trace of one routing decision, persisted
// verbatim into agent_task_queue.routing (JSONB).
type RuntimeRoutingDecision struct {
	Mode            string                  `json:"mode"`
	TaskClass       string                  `json:"task_class"`
	ChosenRuntimeID string                  `json:"chosen_runtime_id"`
	ChosenModel     string                  `json:"chosen_model"`
	Reason          string                  `json:"reason"`
	Candidates      []RoutingCandidateTrace `json:"candidates"`
}

// ChosenRuntime returns the routed runtime id parsed for query params.
// Invalid only when the decision carries no id (never: the fallback always
// fills it from the agent), so MustParse is safe on any decision RouteTask
// produced.
func (d RuntimeRoutingDecision) ChosenRuntime() pgtype.UUID {
	return util.MustParseUUID(d.ChosenRuntimeID)
}

// Marshal encodes the decision for the routing JSONB column. A nil return
// (encoding a plain struct of scalars and strings cannot realistically fail)
// means "leave the column NULL".
func (d RuntimeRoutingDecision) Marshal() []byte {
	encoded, err := json.Marshal(d)
	if err != nil {
		slog.Error("runtime router: marshal decision failed", "error", err)
		return nil
	}
	return encoded
}

// wilsonLowerBound returns the lower bound of the Wilson score confidence
// interval for successes/samples at z=1.96 (95%). It is the conservative
// success estimate: small samples are penalized toward 0, so a perfect record
// on one run never outranks a strong record on hundreds.
func wilsonLowerBound(successes, samples int) float64 {
	if samples <= 0 {
		return 0
	}
	n := float64(samples)
	p := float64(successes) / n
	z2 := wilsonZ95 * wilsonZ95
	denom := 1 + z2/n
	center := p + z2/(2*n)
	margin := wilsonZ95 * math.Sqrt(p*(1-p)/n+z2/(4*n*n))
	return (center - margin) / denom
}

// listIssueLabelNames loads the issue's label names for task classification.
// Fail-soft: a label lookup error must never block an enqueue, so it logs and
// returns an empty slice (the classifier then works off the title alone).
func (s *TaskService) listIssueLabelNames(ctx context.Context, issue db.Issue) []string {
	rows, err := s.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("runtime router: load issue labels failed; classifying from title only",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
		return nil
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

// routingRand returns the service's randomness source for the exploration
// draw, defaulting to a time-seeded source when none was injected (tests
// inject a seeded *rand.Rand for determinism).
func (s *TaskService) routingRand() *rand.Rand {
	if s.RoutingRand != nil {
		return s.RoutingRand
	}
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// routingCandidate is a (runtime, model) pair being evaluated.
type routingCandidate struct {
	runtimeID pgtype.UUID
	provider  string
	model     string
	stats     *db.GetRoutingStatsRow
	trace     RoutingCandidateTrace
	scored    bool
	score     float64
}

// RouteTask picks the (runtime, model) pair for one enqueue of an auto-routed
// agent. It never fails: any degradation (no candidates, query error, cold
// start with no scored history) falls back to the agent's bound runtime and
// model, still recording the full trace so the decision stays auditable.
func (s *TaskService) RouteTask(ctx context.Context, agent db.Agent, issueTitle string, labels []string) RuntimeRoutingDecision {
	taskClass := ClassifyTask(issueTitle, labels)
	decision := RuntimeRoutingDecision{
		Mode:            RoutingModeAuto,
		TaskClass:       taskClass,
		ChosenRuntimeID: util.UUIDToString(agent.RuntimeID),
		ChosenModel:     agent.Model.String,
		Reason:          routingReasonNoCandidates,
		Candidates:      []RoutingCandidateTrace{},
	}

	runtimes, err := s.Queries.ListRoutingCandidateRuntimes(ctx, db.ListRoutingCandidateRuntimesParams{
		WorkspaceID:      agent.WorkspaceID,
		OwnerID:          agent.OwnerID,
		RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		slog.Warn("runtime router: candidate query failed; falling back to agent runtime",
			"agent_id", util.UUIDToString(agent.ID), "error", err)
		decision.Reason = routingReasonQueryFailed
		return decision
	}
	if len(runtimes) == 0 {
		return decision
	}

	stats, err := s.Queries.GetRoutingStats(ctx, db.GetRoutingStatsParams{
		WorkspaceID: agent.WorkspaceID,
		Since:       pgtype.Timestamptz{Time: time.Now().Add(-routingStatsWindow), Valid: true},
	})
	if err != nil {
		slog.Warn("runtime router: stats query failed; falling back to agent runtime",
			"agent_id", util.UUIDToString(agent.ID), "error", err)
		decision.Reason = routingReasonQueryFailed
		return decision
	}

	candidates := buildRoutingCandidates(agent, runtimes, stats, taskClass)
	pool := scoreRoutingCandidates(candidates)
	decision.Candidates = make([]RoutingCandidateTrace, 0, len(candidates))
	for _, c := range candidates {
		decision.Candidates = append(decision.Candidates, c.trace)
	}

	// Epsilon-greedy: with probability epsilon, explore the least-sampled
	// under-sampled candidate; otherwise exploit the best-scored one. With an
	// empty scored pool (cold start) the decision falls back to the agent
	// default — its runs then accrue the samples that bootstrap scoring.
	chosen, explored := chooseRoutingCandidate(candidates, pool, s.routingRand())
	if chosen == nil {
		decision.Reason = routingReasonNoScoredCandidates
		return decision
	}
	decision.ChosenRuntimeID = util.UUIDToString(chosen.runtimeID)
	decision.ChosenModel = chosen.model
	decision.Reason = routingReasonBestScore
	if explored {
		decision.Reason = routingReasonExploreLeastSample
	}
	return decision
}

// buildRoutingCandidates expands the runtime candidate set into
// (runtime, model) pairs and attaches the matching stats row for the task
// class. The model universe per runtime is v1 of the model-catalog problem:
// the live daemon model catalogue (InitiateListModels flow) is not
// synchronously queryable at enqueue time, so candidates are the models
// OBSERVED in task_usage for that runtime, plus the agent's preferred model
// on its bound runtime, plus a "runtime default" (empty model) entry for
// runtimes with no observed models yet.
func buildRoutingCandidates(agent db.Agent, runtimes []db.AgentRuntime, stats []db.GetRoutingStatsRow, taskClass string) []*routingCandidate {
	type modelKey struct {
		provider string
		model    string
	}
	modelsByRuntime := make(map[[16]byte][]modelKey)
	seenByRuntime := make(map[[16]byte]map[modelKey]bool)
	addModel := func(runtimeID pgtype.UUID, provider, model string) {
		key := modelKey{provider: provider, model: model}
		seen := seenByRuntime[runtimeID.Bytes]
		if seen == nil {
			seen = map[modelKey]bool{}
			seenByRuntime[runtimeID.Bytes] = seen
		}
		if seen[key] {
			return
		}
		seen[key] = true
		modelsByRuntime[runtimeID.Bytes] = append(modelsByRuntime[runtimeID.Bytes], key)
	}
	// statsByKey holds the row for the REQUESTED task class only; stats for
	// other classes still contribute their model to the universe above.
	statsByKey := make(map[[16]byte]map[modelKey]*db.GetRoutingStatsRow)
	for i := range stats {
		row := &stats[i]
		addModel(row.RuntimeID, row.Provider, row.Model)
		if row.TaskClass != taskClass {
			continue
		}
		byModel := statsByKey[row.RuntimeID.Bytes]
		if byModel == nil {
			byModel = map[modelKey]*db.GetRoutingStatsRow{}
			statsByKey[row.RuntimeID.Bytes] = byModel
		}
		byModel[modelKey{provider: row.Provider, model: row.Model}] = row
	}

	candidates := []*routingCandidate{}
	for _, rt := range runtimes {
		if rt.ID == agent.RuntimeID && agent.Model.Valid && agent.Model.String != "" {
			// The usage provider string and the runtime provider string are
			// different namespaces (e.g. "openai" vs "codex"), so match the
			// agent's preferred model by NAME against the observed universe
			// first — otherwise the same model would appear as two
			// candidates, one with stats and one without.
			alreadyObserved := false
			for _, m := range modelsByRuntime[rt.ID.Bytes] {
				if m.model == agent.Model.String {
					alreadyObserved = true
					break
				}
			}
			if !alreadyObserved {
				addModel(rt.ID, rt.Provider, agent.Model.String)
			}
		}
		models := modelsByRuntime[rt.ID.Bytes]
		if len(models) == 0 {
			// No observed model: the runtime runs its own default model.
			models = []modelKey{{provider: rt.Provider, model: ""}}
		}
		for _, m := range models {
			c := &routingCandidate{
				runtimeID: rt.ID,
				provider:  m.provider,
				model:     m.model,
				trace: RoutingCandidateTrace{
					RuntimeID: util.UUIDToString(rt.ID),
					Provider:  m.provider,
					Model:     m.model,
				},
			}
			if row := statsByKey[rt.ID.Bytes][m]; row != nil {
				c.stats = row
				c.trace.Samples = int(row.Samples)
				if row.Samples > 0 {
					c.trace.SuccessRate = float64(row.SuccessCount) / float64(row.Samples)
				}
				c.trace.WilsonLower = wilsonLowerBound(int(row.SuccessCount), int(row.Samples))
				if row.CostSamples > 0 {
					avg := row.TotalCostUsdTicks / float64(row.CostSamples) * costTicksPerUSD
					c.trace.AvgCostUSD = &avg
				}
				if row.DurationSamples > 0 {
					avg := row.TotalDurationSecs / float64(row.DurationSamples)
					c.trace.AvgDurationSecs = &avg
				}
			}
			candidates = append(candidates, c)
		}
	}
	return candidates
}

// scoreRoutingCandidates applies the floor guard and computes the weighted
// score for every candidate with enough samples. Returns the scored pool.
func scoreRoutingCandidates(candidates []*routingCandidate) []*routingCandidate {
	pool := []*routingCandidate{}
	for _, c := range candidates {
		samples := c.trace.Samples
		switch {
		case samples >= routingExcludeMinSamples && c.trace.SuccessRate < routingExcludeMaxSuccessRate:
			c.trace.ExcludedReason = "low_success_rate"
		case samples >= routingMinScoredSamples:
			c.scored = true
			pool = append(pool, c)
		}
		// samples < routingMinScoredSamples: exploration-only, no score.
	}
	if len(pool) == 0 {
		return pool
	}

	// Min-max normalize cost and duration over the pool members that have
	// data; members without data take the neutral midpoint.
	normalize := func(value func(*routingCandidate) *float64) map[*routingCandidate]float64 {
		out := map[*routingCandidate]float64{}
		min, max := math.Inf(1), math.Inf(-1)
		anyData := false
		for _, c := range pool {
			if v := value(c); v != nil {
				anyData = true
				min = math.Min(min, *v)
				max = math.Max(max, *v)
			}
		}
		for _, c := range pool {
			v := value(c)
			switch {
			case v == nil || !anyData:
				out[c] = routingUnknownNormMidpoint
			case max == min:
				out[c] = 0
			default:
				out[c] = (*v - min) / (max - min)
			}
		}
		return out
	}
	normCost := normalize(func(c *routingCandidate) *float64 { return c.trace.AvgCostUSD })
	normDuration := normalize(func(c *routingCandidate) *float64 { return c.trace.AvgDurationSecs })

	for _, c := range pool {
		c.score = c.trace.WilsonLower - routingCostWeight*normCost[c] - routingDurationWeight*normDuration[c]
		score := c.score
		c.trace.Score = &score
	}
	return pool
}

// chooseRoutingCandidate implements the epsilon-greedy pick. With probability
// epsilon it explores the least-sampled under-sampled candidate (ties break
// deterministically by candidate-list order); otherwise it exploits the
// highest score. Returns explored=true on the exploration branch. Nil when
// the scored pool is empty — exploration never fires without at least one
// established candidate, so a cold workspace falls back instead of
// distributing tasks over totally unknown runtimes.
func chooseRoutingCandidate(candidates, pool []*routingCandidate, rnd *rand.Rand) (chosen *routingCandidate, explored bool) {
	if len(pool) == 0 {
		return nil, false
	}
	if rnd.Float64() < routingExplorationEpsilon {
		var least *routingCandidate
		for _, c := range candidates {
			if c.scored || c.trace.ExcludedReason != "" {
				continue
			}
			if least == nil || c.trace.Samples < least.trace.Samples {
				least = c
			}
		}
		if least != nil {
			return least, true
		}
	}
	best := pool[0]
	for _, c := range pool[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return best, false
}

// RoutingStamp is what one enqueue writes into a new task's task_class /
// routing columns, plus the runtime the router picked for it.
type RoutingStamp struct {
	// TaskClass is always set: every task is classified so its outcome feeds
	// the routing statistics, fixed-mode agents included.
	TaskClass pgtype.Text
	// Routing is the marshalled RuntimeRoutingDecision, non-nil only for
	// auto-routed agents.
	Routing []byte
	// RuntimeID is the routed runtime. Invalid when the agent is fixed-mode or
	// the router degraded: the caller then keeps whatever runtime it had.
	RuntimeID pgtype.UUID
}

// StampRouting is the single routing decision every task creator goes through:
// classify the work, and for a runtime_routing='auto' agent run the router and
// persist its audit trace. Callers pass whatever text describes the work
// (issue title, chat message, quick-create prompt, autopilot title) and the
// issue labels when they have them.
//
// It never fails: RouteTask degrades to the agent's bound runtime and the
// classifier's worst case is the "general" bucket.
func (s *TaskService) StampRouting(ctx context.Context, agent db.Agent, title string, labels []string) RoutingStamp {
	stamp := RoutingStamp{TaskClass: pgtype.Text{String: ClassifyTask(title, labels), Valid: true}}
	if agent.RuntimeRouting != RoutingModeAuto {
		return stamp
	}
	decision := s.RouteTask(ctx, agent, title, labels)
	stamp.Routing = decision.Marshal()
	stamp.RuntimeID = decision.ChosenRuntime()
	return stamp
}
