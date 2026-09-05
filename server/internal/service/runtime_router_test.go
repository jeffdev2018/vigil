package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWilsonLowerBound(t *testing.T) {
	tests := []struct {
		name      string
		successes int
		samples   int
		want      float64
		tol       float64
	}{
		{name: "no samples", successes: 0, samples: 0, want: 0, tol: 0},
		{name: "perfect single run is heavily discounted", successes: 1, samples: 1, want: 0.2065, tol: 0.001},
		{name: "half of ten", successes: 5, samples: 10, want: 0.2366, tol: 0.001},
		{name: "strong record on hundreds", successes: 190, samples: 200, want: 0.9104, tol: 0.001},
		{name: "all failed", successes: 0, samples: 50, want: 0, tol: 0.0001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wilsonLowerBound(tt.successes, tt.samples)
			if math.Abs(got-tt.want) > tt.tol {
				t.Errorf("wilsonLowerBound(%d, %d) = %f, want %f ± %f", tt.successes, tt.samples, got, tt.want, tt.tol)
			}
		})
	}

	// The core ordering guarantee: a perfect record on one run must NEVER
	// outrank a strong record on hundreds.
	if wilsonLowerBound(1, 1) >= wilsonLowerBound(190, 200) {
		t.Errorf("wilsonLower(1,1)=%f must be < wilsonLower(190,200)=%f",
			wilsonLowerBound(1, 1), wilsonLowerBound(190, 200))
	}
	// Monotonic in successes for a fixed sample count.
	prev := -1.0
	for k := 0; k <= 100; k++ {
		v := wilsonLowerBound(k, 100)
		if v < prev {
			t.Fatalf("wilsonLowerBound not monotonic at k=%d: %f < %f", k, v, prev)
		}
		prev = v
	}
}

// statsRow builds one GetRoutingStatsRow with derived sums matching n runs,
// `successes` of them completed, each costing costTicks and running durSecs.
func statsRow(runtimeID pgtype.UUID, provider, model, taskClass string, samples, successes int, costTicks int64, durSecs float64) db.GetRoutingStatsRow {
	return db.GetRoutingStatsRow{
		RuntimeID:         runtimeID,
		RuntimeName:       "rt",
		Provider:          provider,
		Model:             model,
		TaskClass:         taskClass,
		Samples:           int32(samples),
		SuccessCount:      int32(successes),
		CostSamples:       int32(samples),
		TotalCostUsdTicks: float64(costTicks) * float64(samples),
		DurationSamples:   int32(samples),
		TotalDurationSecs: durSecs * float64(samples),
	}
}

func TestScoreRoutingCandidatesFloorExclusion(t *testing.T) {
	rtA := db.AgentRuntime{ID: util.MustParseUUID("00000000-0000-0000-0000-00000000000a"), Provider: "codex"}
	rtB := db.AgentRuntime{ID: util.MustParseUUID("00000000-0000-0000-0000-00000000000b"), Provider: "codex"}
	agent := db.Agent{RuntimeID: rtA.ID}
	stats := []db.GetRoutingStatsRow{
		// rtB: enough history to be trusted, and it is bad → excluded.
		statsRow(rtB.ID, "openai", "bad-model", TaskClassGeneral, 20, 5, 100, 10),
		// rtA: solid record → the only scored candidate.
		statsRow(rtA.ID, "openai", "good-model", TaskClassGeneral, 20, 19, 100, 10),
	}

	candidates := buildRoutingCandidates(agent, []db.AgentRuntime{rtA, rtB}, stats, TaskClassGeneral)
	pool := scoreRoutingCandidates(candidates)
	if len(pool) != 1 {
		t.Fatalf("scored pool = %d, want 1 (bad candidate must be excluded)", len(pool))
	}
	if pool[0].model != "good-model" {
		t.Fatalf("pool candidate = %q, want good-model", pool[0].model)
	}
	for _, c := range candidates {
		if c.model == "bad-model" && c.trace.ExcludedReason == "" {
			t.Fatal("bad-model trace misses excluded_reason")
		}
		if c.model == "good-model" && c.trace.Score == nil {
			t.Fatal("good-model trace misses score")
		}
	}
}

func TestScoreRoutingCandidatesPrefersHigherWilson(t *testing.T) {
	rtA := db.AgentRuntime{ID: util.MustParseUUID("00000000-0000-0000-0000-00000000000a"), Provider: "codex"}
	rtB := db.AgentRuntime{ID: util.MustParseUUID("00000000-0000-0000-0000-00000000000b"), Provider: "codex"}
	agent := db.Agent{RuntimeID: rtA.ID}
	stats := []db.GetRoutingStatsRow{
		statsRow(rtA.ID, "openai", "m-a", TaskClassBugfix, 200, 130, 100, 10),
		statsRow(rtB.ID, "openai", "m-b", TaskClassBugfix, 200, 190, 100, 10),
	}
	candidates := buildRoutingCandidates(agent, []db.AgentRuntime{rtA, rtB}, stats, TaskClassBugfix)
	pool := scoreRoutingCandidates(candidates)
	if len(pool) != 2 {
		t.Fatalf("scored pool = %d, want 2", len(pool))
	}
	chosen, explored := chooseRoutingCandidate(candidates, pool, rand.New(rand.NewSource(1)))
	if explored {
		t.Fatal("unexpected exploration draw with seed 1")
	}
	if chosen == nil || chosen.model != "m-b" {
		t.Fatalf("chosen = %+v, want m-b (higher wilson lower bound)", chosen)
	}
}

func TestChooseRoutingCandidateExploresAboutTenPercent(t *testing.T) {
	rtA := db.AgentRuntime{ID: util.MustParseUUID("00000000-0000-0000-0000-00000000000a"), Provider: "codex"}
	rtB := db.AgentRuntime{ID: util.MustParseUUID("00000000-0000-0000-0000-00000000000b"), Provider: "codex"}
	agent := db.Agent{RuntimeID: rtA.ID}
	stats := []db.GetRoutingStatsRow{
		statsRow(rtA.ID, "openai", "established", TaskClassGeneral, 100, 95, 100, 10),
		// Under-sampled newcomer: below the scored floor, exploration-only.
		statsRow(rtB.ID, "openai", "newcomer", TaskClassGeneral, 2, 2, 100, 10),
	}
	candidates := buildRoutingCandidates(agent, []db.AgentRuntime{rtA, rtB}, stats, TaskClassGeneral)
	pool := scoreRoutingCandidates(candidates)
	if len(pool) != 1 {
		t.Fatalf("scored pool = %d, want 1", len(pool))
	}

	const n = 5000
	explored := 0
	rnd := rand.New(rand.NewSource(42))
	for i := 0; i < n; i++ {
		chosen, wasExploration := chooseRoutingCandidate(candidates, pool, rnd)
		if chosen == nil {
			t.Fatal("nil choice with non-empty pool")
		}
		if wasExploration {
			explored++
			if chosen.model != "newcomer" {
				t.Fatalf("exploration picked %q, want the least-sampled candidate newcomer", chosen.model)
			}
		} else if chosen.model != "established" {
			t.Fatalf("exploitation picked %q, want established", chosen.model)
		}
	}
	rate := float64(explored) / n
	if rate < 0.07 || rate > 0.13 {
		t.Fatalf("exploration rate = %f over %d draws, want ~= 0.10 (±0.03)", rate, n)
	}
}

func TestChooseRoutingCandidateColdStartReturnsNil(t *testing.T) {
	rtA := db.AgentRuntime{ID: util.MustParseUUID("00000000-0000-0000-0000-00000000000a"), Provider: "codex"}
	agent := db.Agent{RuntimeID: rtA.ID}
	// No stats at all: the single "runtime default" candidate has 0 samples.
	candidates := buildRoutingCandidates(agent, []db.AgentRuntime{rtA}, nil, TaskClassGeneral)
	pool := scoreRoutingCandidates(candidates)
	if len(pool) != 0 {
		t.Fatalf("scored pool = %d, want 0 on cold start", len(pool))
	}
	chosen, explored := chooseRoutingCandidate(candidates, pool, rand.New(rand.NewSource(1)))
	if chosen != nil || explored {
		t.Fatalf("cold start choice = %+v explored=%v, want nil/false (caller falls back)", chosen, explored)
	}
}

func TestRoutingDecisionMarshalRoundTrip(t *testing.T) {
	decision := RuntimeRoutingDecision{
		Mode:            RoutingModeAuto,
		TaskClass:       TaskClassBugfix,
		ChosenRuntimeID: "00000000-0000-0000-0000-00000000000b",
		ChosenModel:     "m-b",
		Reason:          routingReasonBestScore,
		Candidates: []RoutingCandidateTrace{{
			RuntimeID:   "00000000-0000-0000-0000-00000000000b",
			Provider:    "openai",
			Model:       "m-b",
			Samples:     20,
			SuccessRate: 0.9,
			WilsonLower: 0.75,
		}},
	}
	encoded := decision.Marshal()
	if encoded == nil {
		t.Fatal("Marshal returned nil")
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"mode", "task_class", "chosen_runtime_id", "chosen_model", "reason", "candidates"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("trace misses key %q: %s", key, encoded)
		}
	}
	if decoded["mode"] != RoutingModeAuto {
		t.Errorf("mode = %v, want auto", decoded["mode"])
	}
	candidates, ok := decoded["candidates"].([]any)
	if !ok || len(candidates) != 1 {
		t.Fatalf("candidates = %v, want one entry", decoded["candidates"])
	}
	first := candidates[0].(map[string]any)
	for _, key := range []string{"runtime_id", "provider", "model", "samples", "success_rate", "wilson_lower"} {
		if _, ok := first[key]; !ok {
			t.Errorf("candidate trace misses key %q: %v", key, first)
		}
	}
}

// ---- DB-backed router tests ------------------------------------------------

// routingTestFixture seeds a workspace with two online runtimes (A = bound,
// B = alternative) and one auto-routed agent bound to A with model m-a.
type routingTestFixture struct {
	pool      *pgxpool.Pool
	workspace string
	user      string
	runtimeA  string
	runtimeB  string
	agentID   string
}

func newRoutingTestFixture(t *testing.T) routingTestFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	bootstrap := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	userID := bootstrap.User(t,
		fmt.Sprintf("routing-owner-%d", suffix),
		fmt.Sprintf("routing-owner-%d@example.com", suffix),
	)
	workspaceID := bootstrap.Workspace(t,
		fmt.Sprintf("routing-%d", suffix),
		fmt.Sprintf("routing-%d", suffix),
	)
	fx := testutil.New(pool, workspaceID, userID)
	fx.Member(t, workspaceID, userID, "owner")
	runtimeA := fx.Runtime(t, "runtime-a", testutil.Cols{"provider": "codex"})
	runtimeB := fx.Runtime(t, "runtime-b", testutil.Cols{"provider": "codex"})
	agentID := fx.Agent(t, "routing-agent", runtimeA, testutil.Cols{
		"runtime_routing": "auto",
		"model":           "m-a",
	})
	return routingTestFixture{
		pool:      pool,
		workspace: workspaceID,
		user:      userID,
		runtimeA:  runtimeA,
		runtimeB:  runtimeB,
		agentID:   agentID,
	}
}

// seedRoutingRuns inserts n terminal tasks with one usage row each on the
// given runtime, `successes` of them completed, in the given task class.
func seedRoutingRuns(t *testing.T, fx *testutil.Fixture, agentID, runtimeID, taskClass, provider, model string, samples, successes int) {
	t.Helper()
	for i := 0; i < samples; i++ {
		status := "failed"
		if i < successes {
			status = "completed"
		}
		taskID := fx.Task(t, agentID, testutil.Cols{
			"runtime_id":   runtimeID,
			"status":       status,
			"task_class":   taskClass,
			"started_at":   testutil.Raw("now() - interval '2 minutes'"),
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
		})
		fx.Insert(t, "task_usage", testutil.Cols{
			"task_id":        taskID,
			"provider":       provider,
			"model":          model,
			"cost_usd_ticks": 1000,
		})
	}
}

func TestRouteTaskColdStartFallsBackWithTrace(t *testing.T) {
	fx := newRoutingTestFixture(t)
	ctx := context.Background()
	svc := &TaskService{Queries: db.New(fx.pool), RoutingRand: rand.New(rand.NewSource(1))}

	agent, err := svc.Queries.GetAgent(ctx, util.MustParseUUID(fx.agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	decision := svc.RouteTask(ctx, agent, "Improve onboarding flow", nil)

	// Cold start: no history anywhere → total fallback onto the bound pair,
	// but the decision is still traced.
	if decision.ChosenRuntimeID != fx.runtimeA {
		t.Errorf("chosen runtime = %s, want bound fallback %s", decision.ChosenRuntimeID, fx.runtimeA)
	}
	if decision.ChosenModel != "m-a" {
		t.Errorf("chosen model = %q, want agent model m-a", decision.ChosenModel)
	}
	if decision.Reason != routingReasonNoScoredCandidates {
		t.Errorf("reason = %q, want %q", decision.Reason, routingReasonNoScoredCandidates)
	}
	if decision.Mode != RoutingModeAuto || decision.TaskClass != TaskClassGeneral {
		t.Errorf("trace header = (%q, %q), want (auto, general)", decision.Mode, decision.TaskClass)
	}
	// Both runtimes appear in the trace as zero-sample candidates.
	if len(decision.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2 (one per runtime)", len(decision.Candidates))
	}
	for _, c := range decision.Candidates {
		if c.Samples != 0 {
			t.Errorf("candidate %s samples = %d, want 0", c.RuntimeID, c.Samples)
		}
	}
}

func TestRouteTaskPicksStatisticallyBestRuntime(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	// runtime B + model m-b: strong history in the general class.
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 19)
	// runtime A + model m-a: weaker history.
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, "openai", "m-a", 20, 12)

	ctx := context.Background()
	svc := &TaskService{Queries: db.New(fx.pool), RoutingRand: rand.New(rand.NewSource(1))}
	agent, err := svc.Queries.GetAgent(ctx, util.MustParseUUID(fx.agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	decision := svc.RouteTask(ctx, agent, "Improve onboarding flow", nil)
	if decision.Reason != routingReasonBestScore {
		t.Fatalf("reason = %q, want best_score (trace: %+v)", decision.Reason, decision)
	}
	if decision.ChosenRuntimeID != fx.runtimeB {
		t.Errorf("chosen runtime = %s, want %s (best stats)", decision.ChosenRuntimeID, fx.runtimeB)
	}
	if decision.ChosenModel != "m-b" {
		t.Errorf("chosen model = %q, want m-b", decision.ChosenModel)
	}
	// The full candidate list is traced with samples and scores.
	if len(decision.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(decision.Candidates))
	}
	for _, c := range decision.Candidates {
		if c.Samples != 20 {
			t.Errorf("candidate %s/%s samples = %d, want 20", c.RuntimeID, c.Model, c.Samples)
		}
		if c.Score == nil {
			t.Errorf("candidate %s/%s misses score", c.RuntimeID, c.Model)
		}
		if c.AvgCostUSD == nil || c.AvgDurationSecs == nil {
			t.Errorf("candidate %s/%s misses averages", c.RuntimeID, c.Model)
		}
	}
}

func TestRouteTaskSegmentsByTaskClass(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	// B is better for bugfix, A is better for docs — the router must compare
	// candidates within the requested class only.
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassBugfix, "openai", "m-b", 20, 19)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassDocs, "openai", "m-a", 20, 19)

	ctx := context.Background()
	svc := &TaskService{Queries: db.New(fx.pool), RoutingRand: rand.New(rand.NewSource(1))}
	agent, err := svc.Queries.GetAgent(ctx, util.MustParseUUID(fx.agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}

	bugfix := svc.RouteTask(ctx, agent, "Fix the login crash", nil)
	if bugfix.TaskClass != TaskClassBugfix {
		t.Fatalf("task class = %q, want bugfix", bugfix.TaskClass)
	}
	if bugfix.ChosenRuntimeID != fx.runtimeB || bugfix.ChosenModel != "m-b" {
		t.Errorf("bugfix routed to (%s, %q), want (runtime B, m-b)", bugfix.ChosenRuntimeID, bugfix.ChosenModel)
	}

	docs := svc.RouteTask(ctx, agent, "Update the README", nil)
	if docs.TaskClass != TaskClassDocs {
		t.Fatalf("task class = %q, want docs", docs.TaskClass)
	}
	if docs.ChosenRuntimeID != fx.runtimeA || docs.ChosenModel != "m-a" {
		t.Errorf("docs routed to (%s, %q), want (runtime A, m-a)", docs.ChosenRuntimeID, docs.ChosenModel)
	}
}
