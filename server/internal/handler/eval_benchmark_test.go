package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Internal benchmark harness (JEF-276): one suite replayed against several
// (runtime, model) candidates at once. The pin is the whole point — a
// candidate's replays must run on THAT runtime with THAT model, or the two
// columns of numbers are not comparable.

type benchmarkRunsEnvelope struct {
	Runs []BenchmarkRunResponse `json:"runs"`
}

func runBenchmark(t *testing.T, suiteID string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.RunBenchmark, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/eval-suites/"+suiteID+"/benchmark", body), "id", suiteID))
}

func suiteCorpus(t *testing.T, suiteID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.GetEvalSuiteCorpus, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/eval-suites/"+suiteID+"/corpus", nil), "id", suiteID))
}

// benchmarkSuite makes a suite out of the given case titles and returns its id.
func benchmarkSuite(t *testing.T, name string, titles ...string) string {
	t.Helper()
	ids := make([]string, 0, len(titles))
	for _, title := range titles {
		ids = append(ids, evalProvedCase(t, title, "Tests pass"))
	}
	var suite evalSuiteEnvelope
	evalWorkspaceCall(t, testHandler.CreateEvalSuite, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/eval-suites",
		map[string]any{"name": name, "case_ids": ids}).Want(http.StatusCreated).JSON(&suite)
	return suite.Suite.ID
}

// benchmarkCandidateRuntime is a runtime that can host a replay: online, and
// able to confine it (an eval replay is refused outside a container).
func benchmarkCandidateRuntime(t *testing.T, name string) string {
	t.Helper()
	return dbfx.Runtime(t, name, testutil.Cols{
		"sandbox_mode": "none", "sandbox_image": "ghcr.io/acme/bench:1", "device_info": "benchmark fixture",
	})
}

// claimOn claims one task on a runtime and reports the claimed task id, the
// model the daemon was told to run, and whether anything was claimed at all.
func claimOn(t *testing.T, runtimeID string) (taskID, model string, claimed bool) {
	t.Helper()
	var out struct {
		Task *struct {
			ID    string `json:"id"`
			Agent *struct {
				Model string `json:"model"`
			} `json:"agent"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "bench-daemon"),
		"runtimeId", runtimeID)).Want(http.StatusOK).JSON(&out)
	if out.Task == nil {
		return "", "", false
	}
	if out.Task.Agent == nil {
		return out.Task.ID, "", true
	}
	return out.Task.ID, out.Task.Agent.Model, true
}

func TestBenchmarkPinsEachCandidateRuntimeAndModel(t *testing.T) {
	evalCleanup(t)
	// Two classes, so the per-class breakdown has something to split.
	suiteID := benchmarkSuite(t, "two candidates", "fix the crash", "documentation refresh")

	// The agent is bound to a runtime that is NOT a candidate: a benchmark
	// measures the pair it names, not the agent's binding.
	homeRuntime, agentID, versionID := evalAgentWithVersion(t, "bench runner", "pinned instructions", "version-model")
	candidateA := benchmarkCandidateRuntime(t, "candidate A")
	candidateB := benchmarkCandidateRuntime(t, "candidate B")
	// Candidates share the agent's concurrency budget — a benchmark does not
	// exempt itself from max_concurrent_tasks — so measuring two at the same
	// time needs room for two.
	dbfx.Exec(t, `UPDATE agent SET max_concurrent_tasks = 4 WHERE id = $1`, agentID)

	// Everything is validated before a single run is created.
	runBenchmark(t, suiteID, map[string]any{"agent_id": agentID, "agent_version_id": versionID,
		"candidates": []map[string]any{}}).Want(http.StatusBadRequest)
	runBenchmark(t, suiteID, map[string]any{"agent_id": agentID, "agent_version_id": versionID,
		"candidates": []map[string]any{{"runtime_id": uuid.NewString(), "model": "m"}}}).Want(http.StatusUnprocessableEntity)
	runBenchmark(t, suiteID, map[string]any{"agent_id": agentID, "agent_version_id": versionID,
		"candidates": []map[string]any{{"runtime_id": candidateA, "model": "m"}, {"runtime_id": candidateA, "model": "m"}}}).Want(http.StatusBadRequest)
	runBenchmark(t, suiteID, map[string]any{"agent_id": agentID, "agent_version_id": versionID,
		"candidates": []map[string]any{{"runtime_id": candidateA, "model": "m"}}, "baseline_run_id": uuid.NewString()}).Want(http.StatusUnprocessableEntity)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM eval_run WHERE workspace_id = $1`, testWorkspaceID); n != 0 {
		t.Fatalf("a refused benchmark creates no run, got %d", n)
	}

	var started benchmarkRunsEnvelope
	runBenchmark(t, suiteID, map[string]any{
		"agent_id": agentID, "agent_version_id": versionID,
		"candidates": []map[string]any{
			{"runtime_id": candidateA, "model": "model-a"},
			{"runtime_id": candidateB, "model": "model-b"},
		},
	}).Want(http.StatusAccepted).JSON(&started)

	if len(started.Runs) != 2 {
		t.Fatalf("one run per candidate: %+v", started.Runs)
	}
	runA, runB := started.Runs[0], started.Runs[1]
	if !runA.Benchmark || runA.RuntimeID != candidateA || runA.Model != "model-a" || runA.RuntimeName != "candidate A" {
		t.Fatalf("run A carries its pin: %+v", runA)
	}
	if !runB.Benchmark || runB.RuntimeID != candidateB || runB.Model != "model-b" {
		t.Fatalf("run B carries its pin: %+v", runB)
	}
	if len(runA.Cases) != 2 || len(runB.Cases) != 2 {
		t.Fatalf("each candidate replays every case: %d / %d", len(runA.Cases), len(runB.Cases))
	}

	// Every replay task is stamped with its candidate runtime, the benchmark
	// leg role, and the class of the case it replays.
	for _, run := range []BenchmarkRunResponse{runA, runB} {
		want := run.RuntimeID
		for _, c := range run.Cases {
			var runtimeID, legRole, taskClass string
			dbfx.QueryRow(t, `SELECT runtime_id::text, leg_role, task_class FROM agent_task_queue WHERE id = $1`, c.TaskID).
				Scan(&runtimeID, &legRole, &taskClass)
			if runtimeID != want || legRole != "benchmark" {
				t.Fatalf("replay %s is pinned to %s as a benchmark leg, got runtime=%s leg=%s", c.TaskID, want, runtimeID, legRole)
			}
			if taskClass != "bugfix" && taskClass != "docs" {
				t.Fatalf("replay %s classified as %q", c.TaskID, taskClass)
			}
		}
	}

	// The agent's own runtime is not a candidate, so it sees nothing: the
	// pinned tasks stay queued for the runtimes that must measure them.
	if _, _, claimed := claimOn(t, homeRuntime); claimed {
		t.Fatal("a non-candidate runtime claimed a pinned benchmark replay")
	}

	// Candidate A claims one of ITS OWN replays, and is told to run the
	// candidate model rather than the version's.
	claimedA, modelA, ok := claimOn(t, candidateA)
	if !ok {
		t.Fatal("the pinned runtime could not claim its replay")
	}
	if modelA != "model-a" {
		t.Fatalf("the claim runs the candidate model, got %q", modelA)
	}
	belongsToA := false
	for _, c := range runA.Cases {
		if c.TaskID == claimedA {
			belongsToA = true
		}
	}
	if !belongsToA {
		t.Fatalf("candidate A claimed a task that is not its own: %s", claimedA)
	}

	// Candidate B gets its own model on its own replay, on the same suite.
	claimedB, modelB, ok := claimOn(t, candidateB)
	if !ok {
		t.Fatal("the second candidate could not claim its replay")
	}
	if modelB != "model-b" || claimedB == claimedA {
		t.Fatalf("candidate B ran %q on task %s", modelB, claimedB)
	}

	// The two histories are disjoint: a benchmark's per-candidate runs would
	// otherwise fill the eval run history with rows that differ only by a pin
	// that payload does not carry.
	var plain struct {
		Runs []EvalRunResponse `json:"runs"`
	}
	evalWorkspaceCall(t, testHandler.ListEvalRuns, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/eval-runs", nil).
		Want(http.StatusOK).JSON(&plain)
	for _, run := range plain.Runs {
		if run.ID == runA.ID || run.ID == runB.ID {
			t.Fatalf("benchmark run %s is listed in the plain eval run history", run.ID)
		}
	}
	var benchmarks benchmarkRunsEnvelope
	evalWorkspaceCall(t, testHandler.ListBenchmarks, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/benchmarks", nil).
		Want(http.StatusOK).JSON(&benchmarks)
	if len(benchmarks.Runs) != 2 {
		t.Fatalf("both candidates are in the benchmark history: %d runs", len(benchmarks.Runs))
	}
}

func TestBenchmarkSettlementRecordsClassCostAndDurationAndFeedsRoutingStats(t *testing.T) {
	evalCleanup(t)
	suiteID := benchmarkSuite(t, "settled benchmark", "fix the login crash")
	_, agentID, versionID := evalAgentWithVersion(t, "settling bench agent", "pinned instructions", "version-model")
	candidate := benchmarkCandidateRuntime(t, "settling candidate")

	var started benchmarkRunsEnvelope
	runBenchmark(t, suiteID, map[string]any{
		"agent_id": agentID, "agent_version_id": versionID,
		"candidates": []map[string]any{{"runtime_id": candidate, "model": "gpt-bench"}},
	}).Want(http.StatusAccepted).JSON(&started)
	run := started.Runs[0]
	taskID, replayIssue := run.Cases[0].TaskID, run.Cases[0].IssueID

	claimOn(t, candidate)
	testutil.Call(t, testHandler.StartTask, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/start",
		map[string]any{"sandbox_requested": "container", "sandbox_mode": "container"}, testWorkspaceID, "bench-daemon"), "taskId", taskID)).Want(http.StatusOK)

	// The provider priced the run, and it took measurable time: both are what
	// a policy comparison is made of, so both must survive settlement.
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id": taskID, "provider": "OpenAI", "model": "gpt-bench", "cost_usd_ticks": 2_000_000_000, // $0.20
	})
	dbfx.Exec(t, `UPDATE agent_task_queue SET started_at = now() - interval '30 seconds' WHERE id = $1`, taskID)

	for _, c := range listCriteria(t, replayIssue) {
		proveCriterion(t, replayIssue, c.ID, map[string]any{"proof_type": "test", "proof_ref": "go test ./..."}).Want(http.StatusOK)
	}
	testutil.Call(t, testHandler.CompleteTask, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": "done"}, testWorkspaceID, "bench-daemon"), "taskId", taskID)).Want(http.StatusOK)

	var taskClass string
	var cost, duration *int64
	dbfx.QueryRow(t, `SELECT task_class, cost_usd_ticks, duration_seconds FROM eval_run_case WHERE run_id = $1`, run.ID).
		Scan(&taskClass, &cost, &duration)
	if taskClass != "bugfix" {
		t.Fatalf("the case is recorded under the class of its title, got %q", taskClass)
	}
	if cost == nil || *cost != 2_000_000_000 {
		t.Fatalf("settlement records what the replay cost: %v", cost)
	}
	if duration == nil || *duration < 25 {
		t.Fatalf("settlement records how long the replay took: %v", duration)
	}

	// Routing statistics learn from the benchmark: an eval leg is excluded
	// because it grades someone else's work, but a benchmark leg IS the
	// measurement of this (runtime, model) pair.
	stats := testutil.Decode[RuntimeRoutingStatsResponse](t, testHandler.GetRuntimeRoutingStats,
		newRequest(http.MethodGet, "/api/runtimes/routing-stats", nil), http.StatusOK)
	found := false
	for _, row := range stats.Rows {
		if row.RuntimeID == candidate && row.Model == "gpt-bench" && row.TaskClass == "bugfix" {
			found = true
			if row.Samples != 1 || row.SuccessRate != 1 {
				t.Fatalf("the benchmark run is one successful sample: %+v", row)
			}
			// The dashboard says where the evidence came from, and all of it
			// came from the benchmark here.
			if row.BenchmarkSamples != 1 {
				t.Fatalf("benchmark_samples = %d, want 1: %+v", row.BenchmarkSamples, row)
			}
		}
	}
	if !found {
		t.Fatalf("no routing stats row for the benchmarked pair: %+v", stats.Rows)
	}

	// The benchmark history reports the per-class breakdown and the delta
	// against the baseline it was launched with.
	var listed benchmarkRunsEnvelope
	evalWorkspaceCall(t, testHandler.ListBenchmarks, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/benchmarks", nil).
		Want(http.StatusOK).JSON(&listed)
	var got *BenchmarkRunResponse
	for i := range listed.Runs {
		if listed.Runs[i].ID == run.ID {
			got = &listed.Runs[i]
		}
	}
	if got == nil {
		t.Fatalf("the settled benchmark is not in the history: %d runs", len(listed.Runs))
	}
	if got.Score == nil || *got.Score != 100 || got.DeltaScore != nil {
		t.Fatalf("a run with no baseline has no delta: score=%v delta=%v", got.Score, got.DeltaScore)
	}
	class, ok := got.PerClass["bugfix"]
	if !ok || class.Cases != 1 || class.Passed != 1 || class.Score == nil || *class.Score != 100 {
		t.Fatalf("per-class breakdown = %+v", got.PerClass)
	}
	if class.CostUsdTicks == nil || *class.CostUsdTicks != 2_000_000_000 || class.DurationSeconds == nil {
		t.Fatalf("the class carries what it spent: %+v", class)
	}

	// A second benchmark against the first reports the delta between them.
	var second benchmarkRunsEnvelope
	runBenchmark(t, suiteID, map[string]any{
		"agent_id": agentID, "agent_version_id": versionID, "baseline_run_id": run.ID,
		"candidates": []map[string]any{{"runtime_id": candidate, "model": "gpt-bench-2"}},
	}).Want(http.StatusAccepted).JSON(&second)
	if second.Runs[0].BaselineRunID == nil || *second.Runs[0].BaselineRunID != run.ID {
		t.Fatalf("the second run names its baseline: %+v", second.Runs[0])
	}
}

func TestBenchmarkPolicySearchEndpointReportsWithoutApplying(t *testing.T) {
	evalCleanup(t)
	suiteID := benchmarkSuite(t, "policy search suite", "fix the timeout")
	_, agentID, versionID := evalAgentWithVersion(t, "policy bench agent", "pinned instructions", "version-model")
	candidate := benchmarkCandidateRuntime(t, "policy candidate")

	var started benchmarkRunsEnvelope
	runBenchmark(t, suiteID, map[string]any{
		"agent_id": agentID, "agent_version_id": versionID,
		"candidates": []map[string]any{{"runtime_id": candidate, "model": "gpt-bench"}},
	}).Want(http.StatusAccepted).JSON(&started)

	search := func(body map[string]any) *testutil.Response {
		return evalWorkspaceCall(t, testHandler.BenchmarkPolicySearch, http.MethodPost,
			"/api/workspaces/"+testWorkspaceID+"/benchmarks/policy-search", body)
	}
	search(map[string]any{"runs": []string{}}).Want(http.StatusBadRequest)
	// An id that is not a benchmark of this workspace leaves nothing to search.
	search(map[string]any{"runs": []string{uuid.NewString()}}).Want(http.StatusUnprocessableEntity)

	var out struct {
		Grid     []map[string]any `json:"grid"`
		Baseline struct {
			Policy   map[string]any `json:"policy"`
			Baseline bool           `json:"baseline"`
		} `json:"baseline"`
		Improved bool `json:"improved"`
	}
	search(map[string]any{"runs": []string{started.Runs[0].ID}}).Want(http.StatusOK).JSON(&out)
	if len(out.Grid) != 27 {
		t.Fatalf("the grid is the full 3x3x3 cross product, got %d", len(out.Grid))
	}
	if !out.Baseline.Baseline || out.Baseline.Policy["min_samples"] != float64(5) {
		t.Fatalf("the baseline is the router's live policy: %+v", out.Baseline)
	}
	// Nothing settled yet, so nothing was measured and nothing is improved on.
	if out.Improved {
		t.Fatal("an unsettled benchmark cannot recommend a policy change")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`,
		testWorkspaceID, AuditBenchmarkPolicySearch); n != 1 {
		t.Fatalf("the search is audited as a report, got %d entries", n)
	}
}

func TestEvalSuiteCorpusReportsTheClassMix(t *testing.T) {
	evalCleanup(t)
	// Three of four cases are bugfixes: the suite measures bugfix routing
	// more than it measures the candidates.
	skewed := benchmarkSuite(t, "skewed corpus",
		"fix the crash", "fix the timeout", "fix the leak", "documentation refresh")

	var corpus BenchmarkCorpusResponse
	suiteCorpus(t, skewed).Want(http.StatusOK).JSON(&corpus)
	if corpus.Cases != 4 || corpus.SuiteName != "skewed corpus" {
		t.Fatalf("corpus = %+v", corpus)
	}
	if corpus.Classes["bugfix"].Count != 3 || corpus.Classes["bugfix"].Share != 0.75 {
		t.Fatalf("class mix = %+v", corpus.Classes)
	}
	if corpus.Classes["docs"].Count != 1 || corpus.Classes["docs"].Share != 0.25 {
		t.Fatalf("class mix = %+v", corpus.Classes)
	}
	if corpus.Balanced {
		t.Fatal("a suite that is 75% one class is not balanced")
	}

	even := benchmarkSuite(t, "even corpus", "fix the crash", "documentation refresh", "add the feature")
	var balanced BenchmarkCorpusResponse
	suiteCorpus(t, even).Want(http.StatusOK).JSON(&balanced)
	if !balanced.Balanced || len(balanced.Classes) != 3 {
		t.Fatalf("three classes at a third each is balanced: %+v", balanced)
	}

	suiteCorpus(t, uuid.NewString()).Want(http.StatusNotFound)
}
