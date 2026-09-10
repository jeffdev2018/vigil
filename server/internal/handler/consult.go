package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

// ---------------------------------------------------------------------------
// Agent consult (JEF-12)
//
// A running agent task can ask the platform's internal LLM one synchronous
// question mid-run:
//
//   POST /api/consult             task_token only; persists an agent_consult
//                                 row, then calls the LLM directly (no daemon
//                                 run, no chat session) and answers inline
//   GET  /api/consult/{id}        re-read one consult: the owning task's
//                                 task_token, or a member who can see the agent
//   GET  /api/consult?task_id=    a run's consults, in call order (execution
//                                 log); same two callers
//
// State machine: pending -> answered | failed | refused. Refusals (budget
// exhausted, LLM layer disabled) are persisted as born-terminal 'refused'
// rows with a machine-readable reason, and never fail the calling run: the
// refusal is a clean typed response the agent can continue from.
//
// Budget: a lightweight per-task daily COUNT on agent_consult — deliberately
// NOT budget_policy, whose reservations are task-keyed around daemon runs and
// do not fit a synchronous in-run call. The count-then-insert race may admit
// one extra consult over the cap; a hard counter would need serialization the
// feature does not justify.
// ---------------------------------------------------------------------------

// ConsultLLM is the seam CreateAgentConsult uses to reach the internal LLM
// layer, satisfied by *llm.Client. It is an interface (not the concrete
// client) so tests drive the whole path — persistence, budget, refusal states
// — without an HTTP upstream, mirroring service.ChatQuickActionsLLM.
type ConsultLLM interface {
	Enabled() bool
	GenerateJSONWithUsage(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, llm.Usage, error)
}

// consultMaxPerTaskPerDay is the consult budget: rows one task may book on a
// single UTC day, refused ones included. A var so tests can lower it without
// seeding a day's worth of rows.
var consultMaxPerTaskPerDay int64 = 50

// consultMaxErrorLen bounds the LLM error text persisted on a failed row: the
// column feeds the execution log, not a log aggregator.
const consultMaxErrorLen = 500

// consultGeneration budgets the one LLM call. Consults are small utility
// questions, so temperature stays low and the completion cap generous enough
// for a 300-word answer plus JSON framing.
const (
	consultTemperature         = 0.2
	consultMaxCompletionTokens = 4096
)

// consultReason* are the stable machine-readable codes on refused consults.
const (
	consultReasonBudgetExceeded = "consult_budget_exceeded"
	consultReasonLLMDisabled    = "consult_llm_disabled"
)

// consultLLM returns the consult LLM seam, falling back to the handler's base
// client when no dedicated seam was wired (both are the same *llm.Client in
// production; tests inject a stub through the field).
func (h *Handler) consultLLM() ConsultLLM {
	if h.ConsultLLM != nil {
		return h.ConsultLLM
	}
	return h.LLM
}

// consultModel resolves the consult model: MULTICA_CONSULT_MODEL when set,
// the built-in fallback otherwise. It deliberately does NOT inherit the
// deployment default model — consult calls are utility work pinned to a known
// cheap model unless the operator opts out.
func (h *Handler) consultModel() string {
	if m := strings.TrimSpace(h.cfg.ConsultModel); m != "" {
		return m
	}
	return llm.FallbackModel
}

// CreateAgentConsultRequest is the POST /api/consult body.
type CreateAgentConsultRequest struct {
	Question string `json:"question"`
	Context  string `json:"context,omitempty"`
}

// CreateAgentConsultResponse is the answered POST /api/consult body. Cost is
// a pointer because the LLM layer does not report token usage on this path —
// NULL means "not reported", never an estimate.
type CreateAgentConsultResponse struct {
	ConsultID    string `json:"consult_id"`
	Answer       string `json:"answer"`
	Model        string `json:"model"`
	CostUSDTicks *int64 `json:"cost_usd_ticks"`
}

// consultRefusedResponse is the structured body of a refused consult (429
// budget, 503 LLM disabled): `error` for legacy readers, `reason_code` the
// stable machine-readable cause, `consult_id` the persisted refused row.
type consultRefusedResponse struct {
	Error      string `json:"error"`
	ReasonCode string `json:"reason_code"`
	ConsultID  string `json:"consult_id"`
}

// consultTaskScope resolves and authorizes the task_token side of a consult
// request: the X-Task-ID header, the task row, and the calling agent verified
// against both the stamped agent id and the request workspace. Any failure
// writes the response and returns ok=false.
func (h *Handler) consultTaskScope(w http.ResponseWriter, r *http.Request) (db.AgentTaskQueue, db.Agent, bool) {
	taskIDHeader := r.Header.Get("X-Task-ID")
	if taskIDHeader == "" {
		writeError(w, http.StatusBadRequest, "missing task context")
		return db.AgentTaskQueue{}, db.Agent{}, false
	}
	taskUUID, ok := parseUUIDOrBadRequest(w, taskIDHeader, "task id")
	if !ok {
		return db.AgentTaskQueue{}, db.Agent{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, db.Agent{}, false
	}
	// Defense in depth behind the token→task binding: the stamped agent id
	// must name this task's agent, and the agent must live in the stamped
	// workspace, so a wiring regression fails closed instead of letting a
	// consult bill itself to another workspace.
	if stamped := r.Header.Get("X-Agent-ID"); stamped != "" && uuidToString(task.AgentID) != stamped {
		writeError(w, http.StatusForbidden, "task token does not match this task")
		return db.AgentTaskQueue{}, db.Agent{}, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.AgentTaskQueue{}, db.Agent{}, false
	}
	if ws := h.resolveWorkspaceID(r); ws != "" && uuidToString(agent.WorkspaceID) != ws {
		writeError(w, http.StatusForbidden, "task does not belong to this workspace")
		return db.AgentTaskQueue{}, db.Agent{}, false
	}
	return task, agent, true
}

// CreateAgentConsult handles POST /api/consult. Callable only from inside a
// running agent task — the in-handler task_token guard names the credential it
// requires rather than inheriting that guarantee from middleware.
func (h *Handler) CreateAgentConsult(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "consult is only available from within an agent task")
		return
	}
	task, agent, ok := h.consultTaskScope(w, r)
	if !ok {
		return
	}

	var req CreateAgentConsultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}

	model := h.consultModel()
	insertRefused := func(reason string) db.AgentConsult {
		row, err := h.Queries.InsertRefusedAgentConsult(r.Context(), db.InsertRefusedAgentConsultParams{
			WorkspaceID:   agent.WorkspaceID,
			TaskID:        task.ID,
			AgentID:       task.AgentID,
			Model:         model,
			Question:      req.Question,
			RefusalReason: pgtype.Text{String: reason, Valid: true},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record consult refusal")
			return db.AgentConsult{}
		}
		return row
	}

	// Budget first: the cheapest refusal, and the one the cap exists for.
	count, err := h.Queries.CountAgentConsultsForTaskToday(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check consult budget")
		return
	}
	if count >= consultMaxPerTaskPerDay {
		row := insertRefused(consultReasonBudgetExceeded)
		if row.ID.Valid {
			writeJSON(w, http.StatusTooManyRequests, consultRefusedResponse{
				Error:      "daily consult budget exhausted for this task",
				ReasonCode: consultReasonBudgetExceeded,
				ConsultID:  uuidToString(row.ID),
			})
		}
		return
	}

	consultLLM := h.consultLLM()
	if consultLLM == nil || !consultLLM.Enabled() {
		row := insertRefused(consultReasonLLMDisabled)
		if row.ID.Valid {
			writeJSON(w, http.StatusServiceUnavailable, consultRefusedResponse{
				Error:      "consult is not available: no LLM configured",
				ReasonCode: consultReasonLLMDisabled,
				ConsultID:  uuidToString(row.ID),
			})
		}
		return
	}

	row, err := h.Queries.CreateAgentConsult(r.Context(), db.CreateAgentConsultParams{
		WorkspaceID: agent.WorkspaceID,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		Model:       model,
		Question:    req.Question,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record consult")
		return
	}

	raw, usage, err := consultLLM.GenerateJSONWithUsage(
		r.Context(), model,
		service.ConsultSystemPrompt,
		service.BuildConsultUserPrompt(req.Question, req.Context),
		consultTemperature, consultMaxCompletionTokens,
	)
	if err != nil {
		reason := err.Error()
		if len(reason) > consultMaxErrorLen {
			reason = reason[:consultMaxErrorLen]
		}
		if ferr := h.Queries.FinalizeAgentConsultFailure(r.Context(), db.FinalizeAgentConsultFailureParams{
			ID:            row.ID,
			RefusalReason: pgtype.Text{String: reason, Valid: true},
		}); ferr != nil {
			writeError(w, http.StatusInternalServerError, "consult failed and the failure could not be recorded")
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":      "consult generation failed",
			"consult_id": uuidToString(row.ID),
		})
		return
	}

	// Tokens are recorded only when the upstream reported them; cost only when
	// the model has a known rate. Either staying NULL means "not reported" —
	// never a fabricated zero.
	var inputTokens, outputTokens, costTicks pgtype.Int8
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		inputTokens = pgtype.Int8{Int64: usage.InputTokens, Valid: true}
		outputTokens = pgtype.Int8{Int64: usage.OutputTokens, Valid: true}
		if ticks := pricing.EstimateTicks(pricing.Usage{
			Model:        model,
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
		}); ticks > 0 {
			costTicks = pgtype.Int8{Int64: ticks, Valid: true}
		}
	}
	answered, err := h.Queries.FinalizeAgentConsultAnswer(r.Context(), db.FinalizeAgentConsultAnswerParams{
		ID:           row.ID,
		Answer:       pgtype.Text{String: service.ExtractConsultAnswer(raw), Valid: true},
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CostUsdTicks: costTicks,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record consult answer")
		return
	}
	writeJSON(w, http.StatusOK, CreateAgentConsultResponse{
		ConsultID:    uuidToString(answered.ID),
		Answer:       answered.Answer.String,
		Model:        answered.Model,
		CostUSDTicks: int8ToPtr(answered.CostUsdTicks),
	})
}

// AgentConsultResponse is one consult row on the wire, returned by both reads.
type AgentConsultResponse struct {
	ConsultID     string  `json:"consult_id"`
	TaskID        string  `json:"task_id"`
	AgentID       string  `json:"agent_id"`
	Model         string  `json:"model"`
	Question      string  `json:"question"`
	Answer        *string `json:"answer"`
	State         string  `json:"state"`
	RefusalReason *string `json:"refusal_reason"`
	InputTokens   *int64  `json:"input_tokens"`
	OutputTokens  *int64  `json:"output_tokens"`
	CostUSDTicks  *int64  `json:"cost_usd_ticks"`
	CreatedAt     string  `json:"created_at"`
	FinalizedAt   *string `json:"finalized_at,omitempty"`
}

func agentConsultToResponse(row db.AgentConsult) AgentConsultResponse {
	resp := AgentConsultResponse{
		ConsultID:     uuidToString(row.ID),
		TaskID:        uuidToString(row.TaskID),
		AgentID:       uuidToString(row.AgentID),
		Model:         row.Model,
		Question:      row.Question,
		Answer:        textToPtr(row.Answer),
		State:         row.State,
		RefusalReason: textToPtr(row.RefusalReason),
		InputTokens:   int8ToPtr(row.InputTokens),
		OutputTokens:  int8ToPtr(row.OutputTokens),
		CostUSDTicks:  int8ToPtr(row.CostUsdTicks),
		CreatedAt:     timestampToString(row.CreatedAt),
	}
	if row.FinalizedAt.Valid {
		s := timestampToString(row.FinalizedAt)
		resp.FinalizedAt = &s
	}
	return resp
}

// consultReadAuthorized resolves who may read consults: the owning task's
// task_token, or a workspace member who can see the consulting agent. On
// success returns true; on failure the response is already written.
// Cross-workspace and cross-task reads answer 404 (the row does not exist for
// this caller); a member barred by agent visibility gets 403, matching agent
// detail.
func (h *Handler) consultReadAuthorized(w http.ResponseWriter, r *http.Request, row db.AgentConsult) bool {
	if r.Header.Get("X-Actor-Source") == "task_token" {
		if uuidToString(row.TaskID) != r.Header.Get("X-Task-ID") {
			writeError(w, http.StatusNotFound, "consult not found")
			return false
		}
		return true
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" || uuidToString(row.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "consult not found")
		return false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	restricted, ok := h.restrictedAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return false
	}
	if _, hidden := restricted[uuidToString(row.AgentID)]; hidden {
		writeError(w, http.StatusForbidden, "you don't have permission to view this agent's consults")
		return false
	}
	return true
}

// GetAgentConsultByID handles GET /api/consult/{id}.
func (h *Handler) GetAgentConsultByID(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "consult id")
	if !ok {
		return
	}
	row, err := h.Queries.GetAgentConsult(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "consult not found")
		return
	}
	if !h.consultReadAuthorized(w, r, row) {
		return
	}
	writeJSON(w, http.StatusOK, agentConsultToResponse(row))
}

// ListAgentConsults handles GET /api/consult?task_id= — the run's consults in
// call order, for the execution log.
func (h *Handler) ListAgentConsults(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("task_id"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, raw, "task_id")
	if !ok {
		return
	}

	if r.Header.Get("X-Actor-Source") == "task_token" {
		if uuidToString(taskID) != r.Header.Get("X-Task-ID") {
			writeError(w, http.StatusForbidden, "task token does not match this task")
			return
		}
	} else {
		// Member path: authorize against the task's agent before listing — a
		// run's consults inherit that agent's visibility.
		task, err := h.Queries.GetAgentTask(r.Context(), taskID)
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		workspaceID := h.resolveWorkspaceID(r)
		if workspaceID == "" || uuidToString(agent.WorkspaceID) != workspaceID {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		member, ok := h.workspaceMember(w, r, workspaceID)
		if !ok {
			return
		}
		actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
		restricted, ok := h.restrictedAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
		if !ok {
			writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
			return
		}
		if _, hidden := restricted[uuidToString(task.AgentID)]; hidden {
			writeError(w, http.StatusForbidden, "you don't have permission to view this agent's consults")
			return
		}
	}

	rows, err := h.Queries.ListAgentConsultsByTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list consults")
		return
	}
	resp := make([]AgentConsultResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, agentConsultToResponse(row))
	}
	writeJSON(w, http.StatusOK, resp)
}
