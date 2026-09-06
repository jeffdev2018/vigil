package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Internal benchmark harness (JEF-276).
//
// The Eval Lab (K24) replays a suite against ONE agent version and scores it.
// A benchmark replays the SAME suite against several (runtime, model)
// candidates at once, so the only difference between the numbers is the policy
// under test. Each candidate gets its own eval_run with benchmark=true and the
// pair pinned on it: the replay tasks are stamped with that runtime (which is
// what makes them invisible to every other one, ClaimAgentTask already selects
// on runtime_id) and the claim forces that model.
//
// The replays are stamped leg_role='benchmark' rather than 'eval' on purpose:
// an eval leg is excluded from GetRoutingStats because it grades someone
// else's work, but a benchmark leg IS the measurement, so it feeds the routing
// statistics the router later scores on.

const (
	AuditBenchmarkStarted      = "benchmark.started"
	AuditBenchmarkPolicySearch = "benchmark.policy_search"

	// benchmarkMaxCandidates bounds one benchmark: every candidate replays
	// every case, so the run count is candidates × cases.
	benchmarkMaxCandidates = 8
	benchmarkMaxModelLen   = 200

	// corpusDominantShare is the share above which one task class is said to
	// dominate a suite: a corpus that is 3/4 bugfixes measures bugfix routing,
	// not routing.
	corpusDominantShare = 0.60
	// corpusMinCasesForBalance is the size below which "balanced" means
	// nothing — two cases cannot be spread over the classes.
	corpusMinCasesForBalance = 3
)

// BenchmarkClassBreakdown is one task class's slice of a benchmark run.
type BenchmarkClassBreakdown struct {
	Cases  int    `json:"cases"`
	Passed int    `json:"passed"`
	Score  *int32 `json:"score"`
	// CostUsdTicks / DurationSeconds are the totals over the cases that
	// reported them, nil when none did — a provider that never priced a run
	// and a run that never started are not zero.
	CostUsdTicks    *int64 `json:"cost_usd_ticks"`
	DurationSeconds *int64 `json:"duration_seconds"`
}

// BenchmarkRunResponse is one candidate's benchmark run.
type BenchmarkRunResponse struct {
	EvalRunResponse
	Benchmark     bool                               `json:"benchmark"`
	RuntimeID     string                             `json:"runtime_id"`
	RuntimeName   string                             `json:"runtime_name"`
	Model         string                             `json:"model"`
	BaselineRunID *string                            `json:"baseline_run_id"`
	PerClass      map[string]BenchmarkClassBreakdown `json:"per_class"`
	// DeltaScore is this run's score minus its baseline's, nil when either
	// side has no score yet.
	DeltaScore *int32 `json:"delta_score"`
}

// BenchmarkCorpusClass is one class's weight in a suite.
type BenchmarkCorpusClass struct {
	Count int     `json:"count"`
	Share float64 `json:"share"`
}

// BenchmarkCorpusResponse is a suite's mix by task class.
type BenchmarkCorpusResponse struct {
	SuiteID   string                          `json:"suite_id"`
	SuiteName string                          `json:"suite_name"`
	Cases     int                             `json:"cases"`
	Classes   map[string]BenchmarkCorpusClass `json:"classes"`
	// Balanced is false as soon as one class holds more than 60% of a suite
	// of at least 3 cases: the benchmark then measures that class, not the
	// candidates. A suite too small to spread is reported balanced because
	// the question does not apply to it.
	Balanced bool `json:"balanced"`
}

// RunBenchmark — POST /api/eval-suites/{id}/benchmark. One eval run per
// candidate, all replaying the same suite.
func (h *Handler) RunBenchmark(w http.ResponseWriter, r *http.Request) {
	suiteID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "suite id")
	if !ok {
		return
	}
	suite, err := h.Queries.GetEvalSuite(r.Context(), suiteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "eval suite not found")
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(suite.WorkspaceID)); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		AgentID        string `json:"agent_id"`
		AgentVersionID string `json:"agent_version_id"`
		Candidates     []struct {
			RuntimeID string `json:"runtime_id"`
			Model     string `json:"model"`
		} `json:"candidates"`
		BaselineRunID string `json:"baseline_run_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agent, versionUUID, ok := h.benchmarkAgentVersion(r.Context(), w, req.AgentID, req.AgentVersionID, suite.WorkspaceID)
	if !ok {
		return
	}
	if len(req.Candidates) == 0 || len(req.Candidates) > benchmarkMaxCandidates {
		writeError(w, http.StatusBadRequest, "a benchmark needs between 1 and 8 candidates")
		return
	}
	// Every candidate is resolved before anything is created: a benchmark
	// whose third candidate names a foreign runtime must not leave two runs
	// half-started against a suite it then holds busy.
	type candidate struct {
		runtimeID pgtype.UUID
		model     string
	}
	candidates := make([]candidate, 0, len(req.Candidates))
	seen := map[string]bool{}
	for _, c := range req.Candidates {
		runtimeUUID, ok := parseUUIDOrBadRequest(w, c.RuntimeID, "runtime id")
		if !ok {
			return
		}
		runtime, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
		if err != nil || runtime.WorkspaceID != suite.WorkspaceID {
			writeError(w, http.StatusUnprocessableEntity, "runtime not found in this workspace")
			return
		}
		model := strings.TrimSpace(c.Model)
		if len(model) > benchmarkMaxModelLen {
			writeError(w, http.StatusBadRequest, "a candidate model name is at most 200 characters")
			return
		}
		key := uuidToString(runtimeUUID) + "\x00" + model
		if seen[key] {
			writeError(w, http.StatusBadRequest, "the same runtime and model appear twice in the candidates")
			return
		}
		seen[key] = true
		candidates = append(candidates, candidate{runtimeID: runtimeUUID, model: model})
	}
	var baseline pgtype.UUID
	if strings.TrimSpace(req.BaselineRunID) != "" {
		parsed, ok := parseUUIDOrBadRequest(w, req.BaselineRunID, "baseline run id")
		if !ok {
			return
		}
		prior, err := h.Queries.GetEvalRun(r.Context(), parsed)
		if err != nil || prior.WorkspaceID != suite.WorkspaceID {
			writeError(w, http.StatusUnprocessableEntity, "baseline run not found in this workspace")
			return
		}
		baseline = parsed
	}
	if active, err := h.Queries.HasRunningEvalRunForSuite(r.Context(), suite.ID); err == nil && active {
		writeErrorCode(w, http.StatusConflict, ErrCodeEvalRunActive, "a run is already in progress on this suite")
		return
	}
	cases, ids, ok := h.benchmarkSuiteCases(r.Context(), w, suite)
	if !ok {
		return
	}

	runs := make([]BenchmarkRunResponse, 0, len(candidates))
	for _, c := range candidates {
		run, err := h.Queries.CreateBenchmarkRun(r.Context(), db.CreateBenchmarkRunParams{
			ID: dbid.NewV7(), WorkspaceID: suite.WorkspaceID, SuiteID: suite.ID, AgentID: agent.ID,
			AgentVersionID: versionUUID, StartedBy: parseUUID(userID),
			RuntimeID: c.runtimeID, Model: c.model, BaselineRunID: baseline,
		})
		if err != nil {
			slog.Warn("benchmark: create run failed", "suite_id", uuidToString(suite.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record the benchmark run")
			return
		}
		// Take the run back: a candidate whose replays could none of them be
		// enqueued is already finished, and reporting it as running would
		// hold the suite busy on the client's view.
		run, _ = h.startEvalRunCases(r, run, cases, ids, agent, userID)
		runs = append(runs, h.benchmarkRunToResponse(r.Context(), run))
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(suite.WorkspaceID))
	h.audit(r.Context(), suite.WorkspaceID, actorType, actorID, AuditBenchmarkStarted, "eval_suite", suite.ID, map[string]any{
		"agent_id": uuidToString(agent.ID), "agent_version_id": uuidToString(versionUUID),
		"candidates": len(candidates), "cases": len(ids), "baseline_run_id": uuidToPtr(baseline),
	}, nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"runs": runs})
}

// benchmarkAgentVersion resolves the agent and the version to pin, answering
// on the wire itself when either is not this workspace's.
func (h *Handler) benchmarkAgentVersion(ctx context.Context, w http.ResponseWriter, agentID, versionID string, wsID pgtype.UUID) (db.Agent, pgtype.UUID, bool) {
	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "agent id")
	if !ok {
		return db.Agent{}, pgtype.UUID{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: wsID})
	if err != nil || agent.ArchivedAt.Valid {
		writeError(w, http.StatusUnprocessableEntity, "agent not found in this workspace")
		return db.Agent{}, pgtype.UUID{}, false
	}
	versionUUID, ok := parseUUIDOrBadRequest(w, versionID, "agent version id")
	if !ok {
		return db.Agent{}, pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetAgentVersion(ctx, db.GetAgentVersionParams{ID: versionUUID, AgentID: agent.ID}); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "this version does not belong to that agent")
		return db.Agent{}, pgtype.UUID{}, false
	}
	return agent, versionUUID, true
}

// benchmarkSuiteCases loads the suite's cases keyed by id, keeping the suite's
// own order in the returned id list.
func (h *Handler) benchmarkSuiteCases(ctx context.Context, w http.ResponseWriter, suite db.EvalSuite) (map[string]db.EvalCase, []string, bool) {
	ids := evalCaseIDs(suite.CaseIds)
	caseUUIDs := make([]pgtype.UUID, 0, len(ids))
	for _, raw := range ids {
		if id, err := util.ParseUUID(raw); err == nil {
			caseUUIDs = append(caseUUIDs, id)
		}
	}
	rows, err := h.Queries.GetEvalCasesByIDs(ctx, db.GetEvalCasesByIDsParams{WorkspaceID: suite.WorkspaceID, Ids: caseUUIDs})
	if err != nil || len(rows) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "this suite has no case left to run")
		return nil, nil, false
	}
	byID := map[string]db.EvalCase{}
	for _, c := range rows {
		byID[uuidToString(c.ID)] = c
	}
	return byID, ids, true
}

// ListBenchmarks — GET /api/workspaces/{id}/benchmarks.
func (h *Handler) ListBenchmarks(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.evalWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListBenchmarkRuns(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list benchmarks")
		return
	}
	runs := make([]BenchmarkRunResponse, 0, len(rows))
	for _, run := range rows {
		runs = append(runs, h.benchmarkRunToResponse(r.Context(), run))
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// GetEvalSuiteCorpus — GET /api/eval-suites/{id}/corpus. What the suite is
// made of, so a benchmark's numbers can be read for what they measure.
func (h *Handler) GetEvalSuiteCorpus(w http.ResponseWriter, r *http.Request) {
	suiteID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "suite id")
	if !ok {
		return
	}
	suite, err := h.Queries.GetEvalSuite(r.Context(), suiteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "eval suite not found")
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(suite.WorkspaceID)); !ok {
		return
	}
	ids := evalCaseIDs(suite.CaseIds)
	caseUUIDs := make([]pgtype.UUID, 0, len(ids))
	for _, raw := range ids {
		if id, err := util.ParseUUID(raw); err == nil {
			caseUUIDs = append(caseUUIDs, id)
		}
	}
	rows, err := h.Queries.GetEvalCasesByIDs(r.Context(), db.GetEvalCasesByIDsParams{WorkspaceID: suite.WorkspaceID, Ids: caseUUIDs})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the eval cases")
		return
	}
	resp := BenchmarkCorpusResponse{
		SuiteID: uuidToString(suite.ID), SuiteName: suite.Name, Cases: len(rows),
		Classes: map[string]BenchmarkCorpusClass{}, Balanced: true,
	}
	for _, c := range rows {
		class := service.ClassifyTask(c.Title, nil)
		entry := resp.Classes[class]
		entry.Count++
		resp.Classes[class] = entry
	}
	for class, entry := range resp.Classes {
		entry.Share = float64(entry.Count) / float64(len(rows))
		resp.Classes[class] = entry
		if len(rows) >= corpusMinCasesForBalance && entry.Share > corpusDominantShare {
			resp.Balanced = false
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// BenchmarkPolicySearch — POST /api/workspaces/{id}/benchmarks/policy-search.
// Replays the router's scoring offline over the named benchmark runs and
// reports the policy that would have picked best on that evidence. Nothing is
// applied: the router's constants stay where they are, and the audit entry
// records that a human was shown an alternative.
func (h *Handler) BenchmarkPolicySearch(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.evalWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Runs []string `json:"runs"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Runs) == 0 || len(req.Runs) > evalMaxSuiteCases {
		writeError(w, http.StatusBadRequest, "a policy search needs between 1 and 100 runs")
		return
	}
	runUUIDs := make([]pgtype.UUID, 0, len(req.Runs))
	for _, raw := range req.Runs {
		id, ok := parseUUIDOrBadRequest(w, raw, "run id")
		if !ok {
			return
		}
		runUUIDs = append(runUUIDs, id)
	}
	runs, err := h.Queries.GetBenchmarkRunsByIDs(r.Context(), db.GetBenchmarkRunsByIDsParams{WorkspaceID: wsUUID, Ids: runUUIDs})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the benchmark runs")
		return
	}
	if len(runs) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "none of those runs is a benchmark of this workspace")
		return
	}
	results := []service.BenchmarkClassResult{}
	for _, run := range runs {
		results = append(results, h.benchmarkClassResults(r.Context(), run)...)
	}
	search := service.BenchmarkPolicySearch(results)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsUUID))
	h.audit(r.Context(), wsUUID, actorType, actorID, AuditBenchmarkPolicySearch, "workspace", wsUUID, map[string]any{
		"runs": len(runs), "winner": search.Winner.Policy, "baseline": search.Baseline.Policy, "improved": search.Improved,
	}, nil)
	writeJSON(w, http.StatusOK, search)
}

// benchmarkClassResults turns one benchmark run into the per-class candidate
// records the policy search scores. Only settled, measured cases count: a
// pending case has no verdict and an infrastructure failure measured nothing.
func (h *Handler) benchmarkClassResults(ctx context.Context, run db.EvalRun) []service.BenchmarkClassResult {
	rows, err := h.Queries.ListEvalRunCases(ctx, run.ID)
	if err != nil {
		slog.Warn("benchmark: list run cases failed", "run_id", uuidToString(run.ID), "error", err)
		return nil
	}
	type bucket struct {
		cases, passed          int
		costTicks, costCases   int64
		durationSecs, durCases int64
	}
	byClass := map[string]*bucket{}
	classes := []string{}
	for _, c := range rows {
		if c.Status != "passed" && c.Status != "failed" {
			continue
		}
		b := byClass[c.TaskClass]
		if b == nil {
			b = &bucket{}
			byClass[c.TaskClass] = b
			classes = append(classes, c.TaskClass)
		}
		b.cases++
		if c.Status == "passed" {
			b.passed++
		}
		if c.CostUsdTicks.Valid {
			b.costTicks += c.CostUsdTicks.Int64
			b.costCases++
		}
		if c.DurationSeconds.Valid {
			b.durationSecs += int64(c.DurationSeconds.Int32)
			b.durCases++
		}
	}
	sort.Strings(classes)
	out := make([]service.BenchmarkClassResult, 0, len(classes))
	for _, class := range classes {
		b := byClass[class]
		result := service.BenchmarkClassResult{
			RunID: uuidToString(run.ID), RuntimeID: uuidToString(run.RuntimeID), Model: run.Model,
			TaskClass: class, Cases: b.cases, Passed: b.passed,
		}
		if b.costCases > 0 {
			avg := float64(b.costTicks) / float64(b.costCases) * costTicksPerUSD
			result.AvgCostUSD = &avg
		}
		if b.durCases > 0 {
			avg := float64(b.durationSecs) / float64(b.durCases)
			result.AvgDurationSecs = &avg
		}
		out = append(out, result)
	}
	return out
}

// benchmarkRunToResponse adds the pin, the per-class breakdown and the delta
// against the baseline run to the ordinary eval run payload.
func (h *Handler) benchmarkRunToResponse(ctx context.Context, run db.EvalRun) BenchmarkRunResponse {
	rows, err := h.Queries.ListEvalRunCases(ctx, run.ID)
	if err != nil {
		slog.Warn("benchmark: list run cases failed", "run_id", uuidToString(run.ID), "error", err)
	}
	out := BenchmarkRunResponse{
		EvalRunResponse: h.evalRunResponseFrom(ctx, run, rows),
		Benchmark:       run.Benchmark,
		RuntimeID:       uuidToString(run.RuntimeID),
		Model:           run.Model,
		BaselineRunID:   uuidToPtr(run.BaselineRunID),
		PerClass:        map[string]BenchmarkClassBreakdown{},
	}
	if run.RuntimeID.Valid {
		if runtime, err := h.Queries.GetAgentRuntime(ctx, run.RuntimeID); err == nil {
			out.RuntimeName = runtime.Name
		}
	}
	type acc struct {
		cases, passed, scored int
		scoreSum              int32
		cost                  int64
		hasCost               bool
		duration              int64
		hasDuration           bool
	}
	byClass := map[string]*acc{}
	for _, c := range rows {
		a := byClass[c.TaskClass]
		if a == nil {
			a = &acc{}
			byClass[c.TaskClass] = a
		}
		a.cases++
		if c.Status == "passed" {
			a.passed++
		}
		if c.Score.Valid {
			a.scoreSum += c.Score.Int32
			a.scored++
		}
		if c.CostUsdTicks.Valid {
			a.cost += c.CostUsdTicks.Int64
			a.hasCost = true
		}
		if c.DurationSeconds.Valid {
			a.duration += int64(c.DurationSeconds.Int32)
			a.hasDuration = true
		}
	}
	for class, a := range byClass {
		entry := BenchmarkClassBreakdown{Cases: a.cases, Passed: a.passed}
		if a.scored > 0 {
			mean := a.scoreSum / int32(a.scored)
			entry.Score = &mean
		}
		if a.hasCost {
			cost := a.cost
			entry.CostUsdTicks = &cost
		}
		if a.hasDuration {
			duration := a.duration
			entry.DurationSeconds = &duration
		}
		out.PerClass[class] = entry
	}
	if run.BaselineRunID.Valid && run.Score.Valid {
		if prior, err := h.Queries.GetEvalRun(ctx, run.BaselineRunID); err == nil && prior.Score.Valid {
			delta := run.Score.Int32 - prior.Score.Int32
			out.DeltaScore = &delta
		}
	}
	return out
}
