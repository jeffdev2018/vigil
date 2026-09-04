package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Run limits (K03): CRUD on the caps and the per-run status. Enforcement
// lives in service/run_limit.go.

const AuditRunLimitChanged = "run_limit.changed"

type RunLimitPolicyRequest struct {
	ScopeType          string  `json:"scope_type"`
	ScopeID            *string `json:"scope_id"`
	MaxCostUSDTicks    *int64  `json:"max_cost_usd_ticks"`
	MaxDurationSeconds *int32  `json:"max_duration_seconds"`
	MaxTurns           *int32  `json:"max_turns"`
	MaxToolCalls       *int32  `json:"max_tool_calls"`
	WarnBps            *int32  `json:"warn_bps"`
	Action             string  `json:"action"`
}

type RunLimitPolicyResponse struct {
	ID                 string  `json:"id"`
	ScopeType          string  `json:"scope_type"`
	ScopeID            *string `json:"scope_id"`
	MaxCostUSDTicks    *int64  `json:"max_cost_usd_ticks"`
	MaxDurationSeconds *int32  `json:"max_duration_seconds"`
	MaxTurns           *int32  `json:"max_turns"`
	MaxToolCalls       *int32  `json:"max_tool_calls"`
	WarnBps            int32   `json:"warn_bps"`
	Action             string  `json:"action"`
	CreatedAt          string  `json:"created_at"`
}

func runLimitToResponse(p db.RunLimitPolicy) RunLimitPolicyResponse {
	out := RunLimitPolicyResponse{ID: uuidToString(p.ID), ScopeType: p.ScopeType, ScopeID: uuidToPtr(p.ScopeID), WarnBps: p.WarnBps, Action: p.Action, CreatedAt: timestampToString(p.CreatedAt)}
	if p.MaxCostUsdTicks.Valid {
		out.MaxCostUSDTicks = &p.MaxCostUsdTicks.Int64
	}
	if p.MaxDurationSeconds.Valid {
		out.MaxDurationSeconds = &p.MaxDurationSeconds.Int32
	}
	if p.MaxTurns.Valid {
		out.MaxTurns = &p.MaxTurns.Int32
	}
	if p.MaxToolCalls.Valid {
		out.MaxToolCalls = &p.MaxToolCalls.Int32
	}
	return out
}

func positive64(v *int64) pgtype.Int8 {
	if v == nil || *v <= 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func positive32(v *int32) pgtype.Int4 {
	if v == nil || *v <= 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func (h *Handler) runLimitValues(w http.ResponseWriter, req RunLimitPolicyRequest, current *db.RunLimitPolicy) (cost pgtype.Int8, duration, turns, tools pgtype.Int4, warn int32, action string, ok bool) {
	cost, duration, turns, tools = positive64(req.MaxCostUSDTicks), positive32(req.MaxDurationSeconds), positive32(req.MaxTurns), positive32(req.MaxToolCalls)
	warn, action = int32(8000), "enforce"
	if current != nil {
		warn, action = current.WarnBps, current.Action
	}
	if req.WarnBps != nil {
		warn = *req.WarnBps
	}
	if strings.TrimSpace(req.Action) != "" {
		action = strings.TrimSpace(req.Action)
	}
	if warn < 0 || warn > 10_000 || (action != "observe" && action != "enforce") {
		writeError(w, http.StatusBadRequest, "warn_bps must be 0-10000 and action observe or enforce")
		return cost, duration, turns, tools, warn, action, false
	}
	if !cost.Valid && !duration.Valid && !turns.Valid && !tools.Valid {
		writeError(w, http.StatusBadRequest, "a run limit needs at least one cap: max_cost_usd_ticks, max_duration_seconds, max_turns or max_tool_calls")
		return cost, duration, turns, tools, warn, action, false
	}
	return cost, duration, turns, tools, warn, action, true
}

func (h *Handler) ListRunLimitPolicies(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.budgetWorkspace(w, r, false)
	if !ok {
		return
	}
	rows, err := h.Queries.ListRunLimitPolicies(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list run limits")
		return
	}
	out := make([]RunLimitPolicyResponse, 0, len(rows))
	for _, p := range rows {
		out = append(out, runLimitToResponse(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": out, "gates": service.RunLimitGates})
}

func (h *Handler) CreateRunLimitPolicy(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := h.budgetWorkspace(w, r, true)
	if !ok {
		return
	}
	var req RunLimitPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	scopeID, valid := h.validateBudgetScope(r, wsID, req.ScopeType, req.ScopeID)
	if !valid {
		writeError(w, http.StatusBadRequest, "scope_type must be workspace, project or agent with a scope_id of this workspace")
		return
	}
	cost, duration, turns, tools, warn, action, ok := h.runLimitValues(w, req, nil)
	if !ok {
		return
	}
	p, err := h.Queries.CreateRunLimitPolicy(r.Context(), db.CreateRunLimitPolicyParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, ScopeType: req.ScopeType, ScopeID: scopeID, MaxCostUsdTicks: cost, MaxDurationSeconds: duration, MaxTurns: turns, MaxToolCalls: tools, WarnBps: warn, Action: action, CreatedBy: userID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a run limit already exists for this scope; edit it instead")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create run limit")
		return
	}
	h.audit(r.Context(), wsID, "member", uuidToString(userID), AuditRunLimitChanged, "run_limit_policy", p.ID, map[string]any{"scope_type": p.ScopeType, "scope_id": uuidToPtr(p.ScopeID), "action": action, "created": true}, nil)
	writeJSON(w, http.StatusCreated, runLimitToResponse(p))
}

func (h *Handler) UpdateRunLimitPolicy(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := h.budgetWorkspace(w, r, true)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "policy id")
	if !ok {
		return
	}
	current, err := h.Queries.GetRunLimitPolicy(r.Context(), id)
	if err != nil || current.WorkspaceID != wsID {
		writeError(w, http.StatusNotFound, "run limit not found")
		return
	}
	var req RunLimitPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cost, duration, turns, tools, warn, action, ok := h.runLimitValues(w, req, &current)
	if !ok {
		return
	}
	p, err := h.Queries.UpdateRunLimitPolicy(r.Context(), db.UpdateRunLimitPolicyParams{ID: id, MaxCostUsdTicks: cost, MaxDurationSeconds: duration, MaxTurns: turns, MaxToolCalls: tools, WarnBps: warn, Action: action})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update run limit")
		return
	}
	h.audit(r.Context(), wsID, "member", uuidToString(userID), AuditRunLimitChanged, "run_limit_policy", p.ID, map[string]any{"scope_type": p.ScopeType, "action": action}, nil)
	writeJSON(w, http.StatusOK, runLimitToResponse(p))
}

func (h *Handler) DeleteRunLimitPolicy(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := h.budgetWorkspace(w, r, true)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "policy id")
	if !ok {
		return
	}
	current, err := h.Queries.GetRunLimitPolicy(r.Context(), id)
	if err != nil || current.WorkspaceID != wsID {
		writeError(w, http.StatusNotFound, "run limit not found")
		return
	}
	if err := h.Queries.DeleteRunLimitPolicy(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete run limit")
		return
	}
	h.audit(r.Context(), wsID, "member", uuidToString(userID), AuditRunLimitChanged, "run_limit_policy", id, map[string]any{"deleted": true}, nil)
	w.WriteHeader(http.StatusNoContent)
}

type RunLimitEventResponse struct {
	TaskID    string `json:"task_id"`
	Gate      string `json:"gate"`
	Level     string `json:"level"`
	Observed  int64  `json:"observed"`
	Limit     int64  `json:"limit"`
	PolicyID  string `json:"policy_id"`
	CreatedAt string `json:"created_at"`
}

func runLimitEventToResponse(e db.RunLimitEvent) RunLimitEventResponse {
	return RunLimitEventResponse{TaskID: uuidToString(e.TaskID), Gate: e.Gate, Level: e.Level, Observed: e.Observed, Limit: e.LimitValue, PolicyID: uuidToString(e.PolicyID), CreatedAt: timestampToString(e.CreatedAt)}
}

// GetTaskBudgetStatus: GET /api/tasks/{taskId}/budget-status.
func (h *Handler) GetTaskBudgetStatus(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	status, err := h.TaskService.RunLimitStatusFor(r.Context(), task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute run limits")
		return
	}
	events := make([]RunLimitEventResponse, 0, len(status.Events))
	for _, e := range status.Events {
		events = append(events, runLimitEventToResponse(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": uuidToString(task.ID), "status": task.Status, "usage": status.Usage, "gates": status.Gates, "events": events})
}

// ListIssueRunLimitEvents: GET /api/issues/{id}/run-limit-events.
func (h *Handler) ListIssueRunLimitEvents(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListRunLimitEventsForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list run limit events")
		return
	}
	out := make([]RunLimitEventResponse, 0, len(rows))
	for _, e := range rows {
		out = append(out, runLimitEventToResponse(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}
