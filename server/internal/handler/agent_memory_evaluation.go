package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/memoryeval"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type AgentMemoryEvaluationResponse struct {
	ExecutionStatus    string `json:"execution_status"`
	ExecutionRuntimeID string `json:"execution_runtime_id,omitempty"`
	ID                 string `json:"id"`
	MemoryID           string `json:"memory_id"`
	Revision           int32  `json:"revision"`
	UploadedBy         string `json:"uploaded_by"`
	CreatedAt          string `json:"created_at"`
	AdoptedRevision    *int32 `json:"adopted_revision"`
	Eligible           bool   `json:"eligible"`
	Reason             string `json:"reason"`
	// ReportHash is the server receipt fingerprint of the stored report bytes
	// (SHA-256 hex). It is not provider model/billing attestation.
	ReportHash       string             `json:"report_hash"`
	CostStatus       string             `json:"cost_status"`                  // unavailable | estimated | partial
	EstimatedCostUSD *float64           `json:"estimated_cost_usd,omitempty"` // catalog estimate sum; never a provider invoice
	Total            int                `json:"total"`
	BaselinePassed   int                `json:"baseline_passed"`
	CandidatePassed  int                `json:"candidate_passed"`
	Regressions      int                `json:"regressions"`
	Errors           int                `json:"errors"`
	Report           *memoryeval.Report `json:"report,omitempty"`
}

func evaluationResponse(row db.AgentMemoryEvaluation, detail bool) (AgentMemoryEvaluationResponse, error) {
	var report memoryeval.Report
	if err := json.Unmarshal(row.Report, &report); err != nil {
		return AgentMemoryEvaluationResponse{}, err
	}
	eligible, reason := report.Gate()
	result := AgentMemoryEvaluationResponse{ExecutionStatus: row.ExecutionStatus, ExecutionRuntimeID: uuidToString(row.ExecutionRuntimeID), ID: uuidToString(row.ID), MemoryID: uuidToString(row.MemoryID), Revision: row.Revision, UploadedBy: uuidToString(row.UploadedBy), CreatedAt: timestampToString(row.CreatedAt), Eligible: eligible, Reason: reason, ReportHash: row.ReportHash, CostStatus: "unavailable", Total: len(report.Suite.Cases)}
	if (row.ExecutionStatus == "queued" || row.ExecutionStatus == "running") && row.ExecutionDeadline.Valid && !row.ExecutionDeadline.Time.After(time.Now()) {
		result.ExecutionStatus = "failed"
	}
	if row.ExecutionStatus != "imported" && row.ExecutionStatus != "completed" {
		result.Eligible = false
	}
	if row.AdoptedRevision.Valid {
		result.AdoptedRevision = &row.AdoptedRevision.Int32
	}
	var cost float64
	priced, withUsage := 0, 0
	for _, c := range report.Cases {
		if c.Baseline.Status == "passed" {
			result.BaselinePassed++
		}
		if c.Candidate.Status == "passed" {
			result.CandidatePassed++
		}
		if c.Baseline.Status == "passed" && c.Candidate.Status == "failed" {
			result.Regressions++
		}
		if c.Baseline.Status == "error" || c.Candidate.Status == "error" {
			result.Errors++
		}
		for _, o := range []memoryeval.Outcome{c.Baseline, c.Candidate} {
			if o.Runtime != nil && len(o.Runtime.Usage) > 0 {
				withUsage++
			}
			if o.CostUSD != nil && o.CostSource == "catalog_estimate" {
				cost += *o.CostUSD
				priced++
			}
		}
	}
	if priced > 0 {
		rounded, _ := strconv.ParseFloat(strconv.FormatFloat(cost, 'f', 10, 64), 64)
		result.EstimatedCostUSD = &rounded
		result.CostStatus = "estimated"
		if withUsage > priced {
			result.CostStatus = "partial"
		}
	} else if withUsage > 0 {
		result.CostStatus = "partial"
	}
	if detail {
		result.Report = &report
	}
	return result, nil
}

func (h *Handler) evaluationMemoryAccess(w http.ResponseWriter, r *http.Request) (db.Agent, db.AgentMemory, bool) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "memory evaluation reports are only available to human managers")
		return db.Agent{}, db.AgentMemory{}, false
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return agent, db.AgentMemory{}, false
	}
	memory, ok := h.loadAgentMemoryForAgent(w, r, agent)
	return agent, memory, ok
}

func (h *Handler) ListAgentMemoryEvaluations(w http.ResponseWriter, r *http.Request) {
	agent, memory, ok := h.evaluationMemoryAccess(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListAgentMemoryEvaluations(r.Context(), db.ListAgentMemoryEvaluationsParams{WorkspaceID: agent.WorkspaceID, MemoryID: memory.ID})
	if err != nil {
		writeError(w, 500, "failed to read evaluations")
		return
	}
	result := []AgentMemoryEvaluationResponse{}
	for _, row := range rows {
		item, err := evaluationResponse(row, false)
		if err != nil {
			writeError(w, 500, "failed to decode evaluation")
			return
		}
		result = append(result, item)
	}
	writeJSON(w, 200, result)
}

func (h *Handler) GetAgentMemoryEvaluation(w http.ResponseWriter, r *http.Request) {
	agent, memory, ok := h.evaluationMemoryAccess(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "evaluationId"), "evaluation id")
	if !ok {
		return
	}
	row, err := h.Queries.GetAgentMemoryEvaluation(r.Context(), db.GetAgentMemoryEvaluationParams{ID: id, WorkspaceID: agent.WorkspaceID, MemoryID: memory.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "evaluation not found")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to read evaluation")
		return
	}
	response, err := evaluationResponse(row, true)
	if err != nil {
		writeError(w, 500, "failed to decode evaluation")
		return
	}
	writeJSON(w, 200, response)
}

// Imports retain human-supplied evidence, never an attestation of execution.
func (h *Handler) CreateAgentMemoryEvaluation(w http.ResponseWriter, r *http.Request) {
	agent, memory, ok := h.evaluationMemoryAccess(w, r)
	if !ok {
		return
	}
	var report memoryeval.Report
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		writeError(w, 400, "invalid evaluation report (maximum 2 MiB)")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "expected one report")
		return
	}
	if err := validateEvaluationReport(report); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if report.Candidate.ID != uuidToString(memory.ID) || report.Candidate.AgentID != uuidToString(agent.ID) || report.Candidate.WorkspaceID != uuidToString(agent.WorkspaceID) {
		writeError(w, 400, "report belongs to another memory")
		return
	}
	// Computed fields never influence identity or adoption; disregard imported values.
	report.Eligible, report.Reason = report.Gate()
	encoded, err := json.Marshal(report)
	if err != nil {
		writeError(w, 400, "invalid report")
		return
	}
	var values any
	if json.Unmarshal(encoded, &values) != nil || !validEvaluationStrings(values) {
		writeError(w, 400, "report contains invalid text")
		return
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start evaluation import")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, 409, "workspace is unavailable")
		return
	}
	if _, err := qtx.LockAgentForMemoryUpdate(r.Context(), db.LockAgentForMemoryUpdateParams{ID: agent.ID, WorkspaceID: agent.WorkspaceID}); err != nil {
		writeError(w, 409, "agent is unavailable")
		return
	}
	if _, err := qtx.GetAgentMemory(r.Context(), db.GetAgentMemoryParams{ID: memory.ID, WorkspaceID: agent.WorkspaceID}); err != nil {
		writeError(w, 409, "memory is unavailable")
		return
	}
	previous, err := qtx.GetAgentMemoryEvaluationByHash(r.Context(), db.GetAgentMemoryEvaluationByHashParams{WorkspaceID: agent.WorkspaceID, MemoryID: memory.ID, ReportHash: hash})
	if err == nil {
		response, decodeErr := evaluationResponse(previous, false)
		if decodeErr != nil {
			writeError(w, 500, "invalid saved evaluation")
			return
		}
		writeJSON(w, 200, response)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "failed to find evaluation receipt")
		return
	}
	rows, err := qtx.ListAgentMemoryEvaluations(r.Context(), db.ListAgentMemoryEvaluationsParams{WorkspaceID: agent.WorkspaceID, MemoryID: memory.ID})
	if err != nil {
		writeError(w, 500, "failed to count evaluations")
		return
	}
	if len(rows) >= 10 {
		writeError(w, 409, "memory evaluation limit reached (10 reports)")
		return
	}
	ids, err := checkEvaluationVersions(r.Context(), qtx, agent, report)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	row, err := qtx.CreateAgentMemoryEvaluation(r.Context(), db.CreateAgentMemoryEvaluationParams{WorkspaceID: agent.WorkspaceID, AgentID: agent.ID, MemoryID: memory.ID, Revision: report.Candidate.Revision, MemoryIds: ids, Report: encoded, ReportHash: hash, UploadedBy: parseUUID(userID)})
	if err != nil {
		writeError(w, 500, "failed to save evaluation")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit evaluation")
		return
	}
	response, err := evaluationResponse(row, false)
	if err != nil {
		writeError(w, 500, "invalid saved evaluation")
		return
	}
	writeJSON(w, 201, response)
}

func validEvaluationStrings(value any) bool {
	switch v := value.(type) {
	case string:
		return !strings.ContainsRune(v, 0)
	case []any:
		for _, item := range v {
			if !validEvaluationStrings(item) {
				return false
			}
		}
	case map[string]any:
		for _, item := range v {
			if !validEvaluationStrings(item) {
				return false
			}
		}
	}
	return true
}

func (h *Handler) DeleteAgentMemoryEvaluation(w http.ResponseWriter, r *http.Request) {
	agent, memory, ok := h.evaluationMemoryAccess(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "evaluationId"), "evaluation id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start evaluation deletion")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if _, err := q.LockWorkspaceForChatSessionCreate(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, 409, "workspace is unavailable")
		return
	}
	if _, err := q.LockAgentForMemoryUpdate(r.Context(), db.LockAgentForMemoryUpdateParams{ID: agent.ID, WorkspaceID: agent.WorkspaceID}); err != nil {
		writeError(w, 409, "agent is unavailable")
		return
	}
	count, err := q.DeleteAgentMemoryEvaluation(r.Context(), db.DeleteAgentMemoryEvaluationParams{ID: id, WorkspaceID: agent.WorkspaceID, MemoryID: memory.ID})
	if err != nil {
		writeError(w, 500, "failed to delete evaluation")
		return
	}
	if count == 0 {
		writeError(w, 404, "evaluation not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit evaluation deletion")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateEvaluationReport(r memoryeval.Report) error {
	if r.Version != 1 || !r.ValidKind() || r.StartedAt.IsZero() || r.StartedAt.After(time.Now().Add(time.Minute)) || (r.CompletedAt != nil && (r.CompletedAt.Before(r.StartedAt) || r.CompletedAt.After(time.Now().Add(time.Minute)))) {
		return errors.New("invalid report version or execution dates")
	}
	if err := r.Suite.Validate(); err != nil {
		return err
	}
	if len(r.Baseline) > 199 || len(r.Cases) > len(r.Suite.Cases) {
		return errors.New("invalid report size")
	}
	for i, c := range r.Cases {
		if c.ID != r.Suite.Cases[i].ID || c.Split != r.Suite.Cases[i].Split {
			return errors.New("report cases differ from suite")
		}
		for _, o := range []memoryeval.Outcome{c.Baseline, c.Candidate} {
			if o.Runtime != nil {
				if (r.Suite.WorkerProtocol != "multica_runtime_v1" && r.Suite.WorkerProtocol != memoryeval.ConnectedProtocol) || o.Runtime.Validate() != nil || (o.Status != "error" && o.Runtime.Status != "completed") {
					return errors.New("invalid runtime evidence")
				}
			} else if (r.Suite.WorkerProtocol == "multica_runtime_v1" || r.Suite.WorkerProtocol == memoryeval.ConnectedProtocol) && (o.Status == "passed" || o.Status == "failed") {
				return errors.New("runtime evidence is required")
			}
			if o.Status != "" && o.Status != "passed" && o.Status != "failed" && o.Status != "error" {
				return errors.New("invalid execution status")
			}
			if o.DurationMS < 0 || len(o.Artifact) > 64<<10 || len(o.Diagnostic) > 3*(64<<10)+1024 || strings.ContainsRune(o.Artifact, 0) || strings.ContainsRune(o.Diagnostic, 0) {
				return errors.New("invalid execution output")
			}
			if o.HumanInterventions != nil {
				return errors.New("reports cannot attest human effort")
			}
			if o.CostUSD != nil || o.CostSource != "" {
				if r.Kind != "connected_memory_comparison" || o.CostSource != "catalog_estimate" || o.CostUSD == nil || *o.CostUSD < 0 || math.IsNaN(*o.CostUSD) || math.IsInf(*o.CostUSD, 0) {
					return errors.New("only connected catalog cost estimates may be retained")
				}
			}
		}
	}
	return nil
}

func checkEvaluationVersions(ctx context.Context, q *db.Queries, agent db.Agent, report memoryeval.Report) ([]pgtype.UUID, error) {
	ids := []pgtype.UUID{}
	seen := map[string]bool{}
	for i, m := range append([]memoryeval.Memory{report.Candidate}, report.Baseline...) {
		id, err := util.ParseUUID(m.ID)
		if err != nil || m.AgentID != uuidToString(agent.ID) || m.WorkspaceID != uuidToString(agent.WorkspaceID) || m.Revision < 1 || seen[m.ID] || m.Expired || (m.ExpiresAt != nil && !m.ExpiresAt.After(report.StartedAt)) || (i == 0 && m.Status != "pending") || (i > 0 && m.Status != "active") {
			return nil, errors.New("invalid memory snapshot")
		}
		seen[m.ID] = true
		current, err := q.GetAgentMemory(ctx, db.GetAgentMemoryParams{ID: id, WorkspaceID: agent.WorkspaceID})
		if err != nil || current.AgentID != agent.ID {
			return nil, errors.New("a referenced memory is unavailable")
		}
		content, status, revision, expiry := current.Content, current.Status, current.Revision, current.ExpiresAt
		if current.Revision != m.Revision {
			version, err := q.GetAgentMemoryVersion(ctx, db.GetAgentMemoryVersionParams{MemoryID: id, WorkspaceID: agent.WorkspaceID, Revision: m.Revision})
			if err != nil {
				return nil, errors.New("a referenced memory version is unavailable")
			}
			content, status, revision, expiry = version.Content, version.Status, version.Revision, version.ExpiresAt
		}
		if content != m.Content || status != m.Status || revision != m.Revision || expiry.Valid != (m.ExpiresAt != nil) || (expiry.Valid && !expiry.Time.Equal(*m.ExpiresAt)) {
			return nil, errors.New("report differs from the saved memory version")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Called with the agent-memory writer lock held, so baseline changes cannot race
// adoption. Other report fields remain human-supplied, not execution attestation.
func checkEvaluationAdoption(ctx context.Context, q *db.Queries, agent db.Agent, memory db.AgentMemory, id pgtype.UUID) error {
	row, err := q.GetAgentMemoryEvaluation(ctx, db.GetAgentMemoryEvaluationParams{ID: id, WorkspaceID: agent.WorkspaceID, MemoryID: memory.ID})
	if err != nil {
		return errors.New("evaluation is unavailable")
	}
	if row.ExecutionStatus != "imported" && row.ExecutionStatus != "completed" {
		return errors.New("evaluation execution is not complete")
	}
	var report memoryeval.Report
	if json.Unmarshal(row.Report, &report) != nil {
		return errors.New("evaluation cannot be read")
	}
	if eligible, _ := report.Gate(); !eligible {
		return errors.New("evaluation does not pass the adoption gate")
	}
	if row.Revision != memory.Revision || memory.Status != "pending" || (memory.ExpiresAt.Valid && !memory.ExpiresAt.Time.After(time.Now())) {
		return errors.New("candidate changed or expired; evaluate again")
	}
	if _, err := checkEvaluationVersions(ctx, q, agent, report); err != nil {
		return err
	}
	current, err := q.ListAgentMemories(ctx, db.ListAgentMemoriesParams{AgentID: agent.ID, WorkspaceID: agent.WorkspaceID})
	if err != nil {
		return err
	}
	baseline := []string{}
	for _, m := range current {
		if m.ID != memory.ID && m.Status == "active" && (!m.ExpiresAt.Valid || m.ExpiresAt.Time.After(time.Now())) {
			baseline = append(baseline, fmt.Sprintf("%s:%d", uuidToString(m.ID), m.Revision))
		}
	}
	expected := []string{}
	for _, m := range report.Baseline {
		expected = append(expected, fmt.Sprintf("%s:%d", m.ID, m.Revision))
	}
	sort.Strings(baseline)
	sort.Strings(expected)
	if strings.Join(baseline, "\n") != strings.Join(expected, "\n") {
		return errors.New("active memory context changed; evaluate again")
	}
	return nil
}
