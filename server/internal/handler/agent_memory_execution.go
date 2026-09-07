package handler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/memoryeval"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func decodeMemoryExecution(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		writeError(w, 400, "invalid evaluation request")
		return false
	}
	return true
}

func (h *Handler) memoryExecutionRuntime(w http.ResponseWriter, r *http.Request, a db.Agent) (db.AgentRuntime, bool) {
	if !a.RuntimeID.Valid {
		writeError(w, 409, "bind an online runtime to the agent first")
		return db.AgentRuntime{}, false
	}
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, uuidToString(a.RuntimeID))
	if !ok {
		return rt, false
	}
	if rt.WorkspaceID != a.WorkspaceID || !canEditRuntime(member, rt) {
		writeError(w, 403, "runtime owner or workspace admin required")
		return rt, false
	}
	if rt.Status != "online" || !runtimeHasCapability(rt.Metadata, protocol.DaemonCapabilityMemoryEvaluationV1) {
		writeError(w, 409, "update and connect the runtime before evaluating")
		return rt, false
	}
	if rt.Provider != "claude" && rt.Provider != "codex" {
		writeError(w, 422, "connected evaluation supports Claude and Codex runtimes")
		return rt, false
	}
	if strings.TrimSpace(a.Model.String) == "" {
		writeError(w, 409, "select an explicit agent model first")
		return rt, false
	}
	return rt, true
}

func memoryExecutionConfigHash(a db.Agent, rt db.AgentRuntime) string {
	raw, _ := json.Marshal([]string{uuidToString(rt.ID), rt.Provider, a.Model.String, a.ThinkingLevel.String, os.Getenv("MULTICA_MEMORY_CODE_IMAGE")})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (h *Handler) GetMemoryExecutionConfig(w http.ResponseWriter, r *http.Request) {
	a, _, ok := h.evaluationMemoryAccess(w, r)
	if !ok {
		return
	}
	rt, ok := h.memoryExecutionRuntime(w, r, a)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"runtime_id": uuidToString(rt.ID), "provider": rt.Provider, "model": a.Model.String, "effort": a.ThinkingLevel.String, "config_hash": memoryExecutionConfigHash(a, rt), "max_cases": 8, "timeout_seconds": 60, "check_modes": memoryExecutionCheckModes()})
}

func memoryExecutionCheckModes() []string {
	modes := []string{"exact", "json"}
	if strings.TrimSpace(os.Getenv("MULTICA_MEMORY_CODE_IMAGE")) != "" {
		modes = append(modes, "javascript")
	}
	return modes
}

func memoryExecutionSnapshot(m db.AgentMemory) memoryeval.Memory {
	out := memoryeval.Memory{ID: uuidToString(m.ID), AgentID: uuidToString(m.AgentID), WorkspaceID: uuidToString(m.WorkspaceID), Content: m.Content, Revision: m.Revision, Status: m.Status}
	if m.ExpiresAt.Valid {
		t := m.ExpiresAt.Time
		out.ExpiresAt = &t
		out.Expired = !t.After(time.Now())
	}
	return out
}

func (h *Handler) StartMemoryExecution(w http.ResponseWriter, r *http.Request) {
	a, m, ok := h.evaluationMemoryAccess(w, r)
	if !ok {
		return
	}
	user, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		ConfigHash       string                     `json:"config_hash"`
		RequestID        string                     `json:"request_id"`
		ExpectedRevision int32                      `json:"expected_revision"`
		Cases            []memoryeval.ConnectedCase `json:"cases"`
	}
	if !decodeMemoryExecution(w, r, &req) {
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, req.RequestID, "request_id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start evaluation")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if _, err = q.LockWorkspaceForChatSessionCreate(r.Context(), a.WorkspaceID); err != nil {
		writeError(w, 409, "workspace unavailable")
		return
	}
	if _, err = q.LockAgentForMemoryUpdate(r.Context(), db.LockAgentForMemoryUpdateParams{ID: a.ID, WorkspaceID: a.WorkspaceID}); err != nil {
		writeError(w, 409, "agent unavailable")
		return
	}
	previous, err := q.GetAgentMemoryEvaluationRequest(r.Context(), db.GetAgentMemoryEvaluationRequestParams{WorkspaceID: a.WorkspaceID, MemoryID: m.ID, ExecutionRequestID: requestID})
	if err == nil {
		var frozen memoryeval.Report
		if json.Unmarshal(previous.Report, &frozen) != nil || frozen.Candidate.Revision != req.ExpectedRevision || !reflect.DeepEqual(frozen.Suite.TextCases, req.Cases) {
			writeError(w, 409, "request identity already belongs to different inputs")
			return
		}
		response, e := evaluationResponse(previous, false)
		if e != nil {
			writeError(w, 500, "invalid saved evaluation")
			return
		}
		writeJSON(w, 200, response)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "failed to check request")
		return
	}
	a, err = q.GetAgent(r.Context(), a.ID)
	if err != nil {
		writeError(w, 409, "agent unavailable")
		return
	}
	rt, ok := h.memoryExecutionRuntime(w, r, a)
	if !ok {
		return
	}
	if req.ConfigHash != memoryExecutionConfigHash(a, rt) {
		writeError(w, 409, "runtime configuration changed; refresh before starting")
		return
	}
	m, err = q.GetAgentMemory(r.Context(), db.GetAgentMemoryParams{ID: m.ID, WorkspaceID: a.WorkspaceID})
	if err != nil || m.Revision != req.ExpectedRevision || m.Status != "pending" || (m.ExpiresAt.Valid && !m.ExpiresAt.Time.After(time.Now())) {
		writeError(w, 409, "memory changed or expired")
		return
	}
	rows, err := q.ListAgentMemoryEvaluations(r.Context(), db.ListAgentMemoryEvaluationsParams{WorkspaceID: a.WorkspaceID, MemoryID: m.ID})
	if err != nil {
		writeError(w, 500, "failed to count evaluations")
		return
	}
	if len(rows) >= 10 {
		writeError(w, 409, "memory evaluation limit reached (10 reports)")
		return
	}
	for _, row := range rows {
		if (row.ExecutionStatus == "queued" || row.ExecutionStatus == "running") && row.ExecutionDeadline.Time.After(time.Now()) {
			writeError(w, 409, "an evaluation is already active for this memory")
			return
		}
	}
	workspace, err := q.GetWorkspace(r.Context(), a.WorkspaceID)
	if err != nil {
		writeError(w, 409, "workspace unavailable")
		return
	}
	report := memoryeval.Report{Version: 1, Kind: "connected_memory_comparison", StartedAt: time.Now().UTC(), Candidate: memoryExecutionSnapshot(m), Baseline: []memoryeval.Memory{}, Suite: memoryeval.Suite{WorkerProtocol: memoryeval.ConnectedProtocol, TimeoutSeconds: 60, Worker: []string{}, Verifier: []string{}, Connected: &memoryeval.ConnectedConfig{Provider: rt.Provider, Model: a.Model.String, Effort: a.ThinkingLevel.String, AgentName: a.Name, Instructions: a.Instructions, WorkspaceContext: workspace.Context.String}, TextCases: req.Cases}}
	for _, c := range req.Cases {
		if c.Check == "javascript" {
			report.Suite.CodeImage = os.Getenv("MULTICA_MEMORY_CODE_IMAGE")
		}
		report.Suite.Cases = append(report.Suite.Cases, memoryeval.Case{ID: c.ID, Split: c.Split, Input: c.ID, Checks: c.ID})
	}
	if err = report.Suite.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	memories, err := q.ListAgentMemories(r.Context(), db.ListAgentMemoriesParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID})
	if err != nil {
		writeError(w, 500, "failed to freeze memories")
		return
	}
	for _, memory := range memories {
		if memory.Status == "active" && (!memory.ExpiresAt.Valid || memory.ExpiresAt.Time.After(report.StartedAt)) {
			report.Baseline = append(report.Baseline, memoryExecutionSnapshot(memory))
		}
	}
	if len(report.Baseline) > 199 {
		writeError(w, 409, "too many active memories")
		return
	}
	report.Cases = memoryeval.ConnectedComparisons(report.Suite)
	ids, err := checkEvaluationVersions(r.Context(), q, a, report)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	raw, err := json.Marshal(report)
	if err != nil {
		writeError(w, 400, "invalid report")
		return
	}
	row, err := q.CreateAgentMemoryEvaluation(r.Context(), db.CreateAgentMemoryEvaluationParams{WorkspaceID: a.WorkspaceID, AgentID: a.ID, MemoryID: m.ID, Revision: m.Revision, MemoryIds: ids, Report: raw, ReportHash: fmt.Sprintf("%x", sha256.Sum256(raw)), UploadedBy: parseUUID(user)})
	if err != nil {
		writeError(w, 500, "failed to save evaluation")
		return
	}
	row, err = q.EnqueueAgentMemoryEvaluation(r.Context(), db.EnqueueAgentMemoryEvaluationParams{ID: row.ID, ExecutionRuntimeID: rt.ID, ExecutionRequestID: requestID})
	if err != nil {
		writeError(w, 500, "failed to queue evaluation")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit evaluation")
		return
	}
	h.requestDaemonPendingWork(uuidToString(rt.ID), protocol.PendingWorkKindMemoryEvaluation)
	response, err := evaluationResponse(row, false)
	if err != nil {
		writeError(w, 500, "invalid saved evaluation")
		return
	}
	writeJSON(w, 201, response)
}

func (h *Handler) CancelMemoryExecution(w http.ResponseWriter, r *http.Request) {
	a, m, ok := h.evaluationMemoryAccess(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "evaluationId"), "evaluation_id")
	if !ok {
		return
	}
	n, err := h.Queries.CancelAgentMemoryEvaluation(r.Context(), db.CancelAgentMemoryEvaluationParams{ID: id, WorkspaceID: a.WorkspaceID, MemoryID: m.ID})
	if err != nil {
		writeError(w, 500, "failed to cancel evaluation")
		return
	}
	if n == 0 {
		writeError(w, 409, "evaluation is no longer active")
		return
	}
	w.WriteHeader(204)
}

// Paid evaluation work requires control of the assigned runtime, not merely
// membership in its workspace. Legacy user tokens retain the same owner/admin
// authority as the human launch path; daemon tokens must match its daemon ID.
func (h *Handler) requireMemoryExecutionRuntime(w http.ResponseWriter, r *http.Request) (db.AgentRuntime, bool) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return rt, false
	}
	if middleware.DaemonWorkspaceIDFromContext(r.Context()) != "" {
		daemonID := middleware.DaemonIDFromContext(r.Context())
		if daemonID == "" || !rt.DaemonID.Valid || rt.DaemonID.String != daemonID {
			writeError(w, http.StatusForbidden, "runtime belongs to another daemon")
			return rt, false
		}
		return rt, true
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return rt, false
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "runtime owner or workspace admin required")
		return rt, false
	}
	return rt, true
}

func (h *Handler) ClaimMemoryExecution(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireMemoryExecutionRuntime(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "evaluationId"), "evaluation_id")
	if !ok {
		return
	}
	row, err := h.Queries.ClaimAgentMemoryEvaluation(r.Context(), db.ClaimAgentMemoryEvaluationParams{ID: id, ExecutionRuntimeID: rt.ID})
	if err != nil {
		writeError(w, 409, "evaluation already claimed or unavailable")
		return
	}
	var report memoryeval.Report
	if json.Unmarshal(row.Report, &report) != nil || report.Suite.Validate() != nil || report.Suite.Connected == nil || report.Suite.Connected.Provider != rt.Provider {
		writeError(w, 409, "invalid evaluation configuration")
		return
	}
	job := memoryeval.ConnectedJob{ID: uuidToString(row.ID), Config: *report.Suite.Connected, Candidate: report.Candidate.Content, Baseline: []string{}}
	for _, c := range report.Suite.TextCases {
		c.Expected = ""
		job.Cases = append(job.Cases, c)
	}
	for _, m := range report.Baseline {
		job.Baseline = append(job.Baseline, m.Content)
	}
	writeJSON(w, 200, job)
}

func (h *Handler) ReportMemoryExecution(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireMemoryExecutionRuntime(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "evaluationId"), "evaluation_id")
	if !ok {
		return
	}
	var result memoryeval.ConnectedResult
	if !decodeMemoryExecution(w, r, &result) {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to save execution")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	row, err := q.GetRuntimeMemoryEvaluation(r.Context(), db.GetRuntimeMemoryEvaluationParams{ID: id, ExecutionRuntimeID: rt.ID})
	if err != nil {
		writeError(w, 404, "evaluation unavailable")
		return
	}
	if row.ExecutionStatus != "running" && row.ExecutionStatus != "completed" && row.ExecutionStatus != "failed" {
		writeError(w, 409, "evaluation stopped")
		return
	}
	var report memoryeval.Report
	if json.Unmarshal(row.Report, &report) != nil || report.Suite.Validate() != nil || report.Suite.Connected == nil || len(report.Cases) != len(report.Suite.Cases) {
		writeError(w, 500, "invalid saved report")
		return
	}
	next := 0
	for _, c := range report.Cases {
		if c.Baseline.Status != "" {
			next++
		}
		if c.Candidate.Status != "" {
			next++
		}
	}
	if result.Index < 0 || result.Index >= 2*len(report.Cases) {
		writeError(w, 400, "invalid result index")
		return
	}
	if _, err := memoryeval.GradeConnected(memoryeval.ConnectedCase{}, result, nil, ""); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if result.Runtime != nil && (result.Runtime.Provider != report.Suite.Connected.Provider || result.Runtime.RequestedModel != report.Suite.Connected.Model || result.Runtime.RequestedEffort != report.Suite.Connected.Effort) {
		writeError(w, 400, "runtime configuration differs from request")
		return
	}
	check := report.Suite.TextCases[result.Index/2]
	var observation *memoryeval.CodeObservation
	if check.Check == "javascript" && !result.Failed && result.Runtime.Validate() == nil && result.Runtime.Status == "completed" {
		if result.Index < next {
			observation = report.Cases[result.Index/2].Baseline.Code
			if result.Index%2 == 1 {
				observation = report.Cases[result.Index/2].Candidate.Code
			}
		} else if result.Index == next && row.ExecutionStatus == "running" && row.ExecutionDeadline.Time.After(time.Now()) {
			observation = memoryeval.VerifyJavaScript(r.Context(), check, result.Artifact, report.Suite.CodeImage)
		} else {
			writeError(w, 409, "evaluation stopped or result out of order")
			return
		}
	}
	outcome, err := memoryeval.GradeConnected(check, result, observation, report.Suite.CodeImage)
	if err != nil {
		if result.Index < next {
			writeError(w, 409, "reported result changed")
			return
		}
		writeError(w, 400, err.Error())
		return
	}
	outcome = applyCatalogCostEstimate(outcome)
	if result.Index < next {
		previous := report.Cases[result.Index/2].Baseline
		if result.Index%2 == 1 {
			previous = report.Cases[result.Index/2].Candidate
		}
		if !reflect.DeepEqual(previous, outcome) {
			writeError(w, 409, "reported result changed")
			return
		}
		writeJSON(w, 200, map[string]string{"status": row.ExecutionStatus})
		return
	}
	if result.Index != next || row.ExecutionStatus != "running" || !row.ExecutionDeadline.Time.After(time.Now()) {
		writeError(w, 409, "evaluation stopped or result out of order")
		return
	}
	if next%2 == 0 {
		report.Cases[next/2].Baseline = outcome
	} else {
		report.Cases[next/2].Candidate = outcome
	}
	status := "running"
	if outcome.Status == "error" {
		status = "failed"
	} else if next+1 == 2*len(report.Cases) {
		status = "completed"
		now := time.Now().UTC()
		report.CompletedAt = &now
		report.Eligible, report.Reason = report.Gate()
	}
	raw, err := json.Marshal(report)
	if err != nil || len(raw) > 2<<20 {
		writeError(w, 400, "report is too large")
		return
	}
	_, err = q.SaveRuntimeMemoryEvaluation(r.Context(), db.SaveRuntimeMemoryEvaluationParams{ID: id, ExecutionRuntimeID: rt.ID, Report: raw, ReportHash: fmt.Sprintf("%x", sha256.Sum256(raw)), ExecutionStatus: status})
	if err != nil {
		writeError(w, 409, "evaluation stopped")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit result")
		return
	}
	writeJSON(w, 200, map[string]string{"status": status})
}

// applyCatalogCostEstimate fills CostUSD from Multica catalog rates when every
// reported usage model is priced. This is never a provider invoice.
func applyCatalogCostEstimate(o memoryeval.Outcome) memoryeval.Outcome {
	if o.CostUSD != nil || o.Runtime == nil || len(o.Runtime.Usage) == 0 {
		return o
	}
	var total float64
	for model, u := range o.Runtime.Usage {
		amount, ok := obsmetrics.EstimateTokenUsageUSD(model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
		if !ok {
			return o
		}
		total += amount
	}
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(total, 'f', 10, 64), 64)
	if err != nil {
		return o
	}
	o.CostUSD = &rounded
	o.CostSource = "catalog_estimate"
	return o
}
