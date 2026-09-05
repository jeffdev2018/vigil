package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type budgetPolicyResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	ScopeType     string  `json:"scope_type"`
	ScopeID       *string `json:"scope_id"`
	LimitUSDTicks int64   `json:"limit_usd_ticks"`
	Period        string  `json:"period"`
	WarnBPS       int32   `json:"warn_bps"`
	Action        string  `json:"action"`
	Revision      int64   `json:"revision"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type budgetStatusResponse struct {
	Policy           budgetPolicyResponse `json:"policy"`
	SpentUSDTicks    int64                `json:"spent_usd_ticks"`
	ReservedUSDTicks int64                `json:"reserved_usd_ticks"`
	PeriodStart      string               `json:"period_start"`
	PeriodEnd        string               `json:"period_end"`
	Reached          bool                 `json:"reached"`
	OverrideExpires  *string              `json:"override_expires_at"`
}

type createBudgetPolicyRequest struct {
	ScopeType     string  `json:"scope_type"`
	ScopeID       *string `json:"scope_id"`
	LimitUSDTicks int64   `json:"limit_usd_ticks"`
	Period        string  `json:"period"`
	WarnBPS       *int32  `json:"warn_bps"`
	Action        string  `json:"action"`
}

type updateBudgetPolicyRequest struct {
	LimitUSDTicks int64  `json:"limit_usd_ticks"`
	Period        string `json:"period"`
	WarnBPS       int32  `json:"warn_bps"`
	Action        string `json:"action"`
	Revision      int64  `json:"revision"`
}

func budgetPolicyToResponse(policy db.BudgetPolicy) budgetPolicyResponse {
	return budgetPolicyResponse{
		ID: uuidToString(policy.ID), WorkspaceID: uuidToString(policy.WorkspaceID),
		ScopeType: policy.ScopeType, ScopeID: uuidToPtr(policy.ScopeID),
		LimitUSDTicks: policy.LimitUsdTicks, Period: policy.Period,
		WarnBPS: policy.WarnBps, Action: policy.Action, Revision: policy.Revision,
		CreatedAt: timestampToString(policy.CreatedAt), UpdatedAt: timestampToString(policy.UpdatedAt),
	}
}

func (h *Handler) budgetWorkspace(w http.ResponseWriter, r *http.Request, write bool) (pgtype.UUID, pgtype.UUID, bool) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	roles := []string{"owner", "admin", "member"}
	if write {
		roles = roles[:2]
	}
	member, ok := h.requireWorkspaceRole(w, r, uuidToString(workspaceID), "workspace not found", roles...)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceID, member.UserID, true
}

func validBudgetSettings(limit int64, period string, warn int32, action string) bool {
	return limit > 0 && (period == "daily" || period == "weekly" || period == "monthly") &&
		warn >= 0 && warn <= 10_000 && (action == "observe" || action == "enforce")
}

func (h *Handler) validateBudgetScope(r *http.Request, workspaceID pgtype.UUID, scopeType string, rawScopeID *string) (pgtype.UUID, bool) {
	if scopeType == "workspace" {
		return pgtype.UUID{}, rawScopeID == nil || strings.TrimSpace(*rawScopeID) == ""
	}
	if rawScopeID == nil || strings.TrimSpace(*rawScopeID) == "" {
		return pgtype.UUID{}, false
	}
	scopeID, err := util.ParseUUID(strings.TrimSpace(*rawScopeID))
	if err != nil {
		return pgtype.UUID{}, false
	}
	switch scopeType {
	case "project":
		_, err = h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: scopeID, WorkspaceID: workspaceID})
	case "agent":
		_, err = h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: scopeID, WorkspaceID: workspaceID})
	default:
		return pgtype.UUID{}, false
	}
	return scopeID, err == nil
}

func (h *Handler) ListBudgetPolicies(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.budgetWorkspace(w, r, false)
	if !ok {
		return
	}
	policies, err := h.Queries.ListBudgetPolicies(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list budget policies")
		return
	}
	response := make([]budgetPolicyResponse, len(policies))
	for i, policy := range policies {
		response[i] = budgetPolicyToResponse(policy)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) GetBudgetStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.budgetWorkspace(w, r, false)
	if !ok {
		return
	}
	scope := service.BudgetScope{WorkspaceID: workspaceID}
	for raw, target := range map[string]*pgtype.UUID{"project_id": &scope.ProjectID, "agent_id": &scope.AgentID} {
		if value := r.URL.Query().Get(raw); value != "" {
			parsed, valid := parseUUIDOrBadRequest(w, value, raw)
			if !valid {
				return
			}
			*target = parsed
		}
	}
	statuses, err := h.BudgetService.Status(r.Context(), scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load budget status")
		return
	}
	response := make([]budgetStatusResponse, len(statuses))
	for i, status := range statuses {
		var override *string
		if status.OverrideExpiresAt != nil {
			formatted := status.OverrideExpiresAt.UTC().Format(time.RFC3339)
			override = &formatted
		}
		response[i] = budgetStatusResponse{Policy: budgetPolicyToResponse(status.Policy), SpentUSDTicks: status.SpentTicks,
			ReservedUSDTicks: status.ReservedTicks, PeriodStart: status.PeriodStart.Format(time.RFC3339),
			PeriodEnd: status.PeriodEnd.Format(time.RFC3339), Reached: status.Reached, OverrideExpires: override}
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) CreateBudgetPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.budgetWorkspace(w, r, true)
	if !ok {
		return
	}
	var req createBudgetPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	warn := int32(8000)
	if req.WarnBPS != nil {
		warn = *req.WarnBPS
	}
	if req.Action == "" {
		req.Action = "enforce"
	}
	if !validBudgetSettings(req.LimitUSDTicks, req.Period, warn, req.Action) {
		writeError(w, http.StatusBadRequest, "invalid budget settings")
		return
	}
	scopeID, valid := h.validateBudgetScope(r, workspaceID, req.ScopeType, req.ScopeID)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid budget scope")
		return
	}
	policy, err := h.Queries.CreateBudgetPolicy(r.Context(), db.CreateBudgetPolicyParams{ID: dbid.NewV7(), WorkspaceID: workspaceID,
		ScopeType: req.ScopeType, ScopeID: scopeID, LimitUsdTicks: req.LimitUSDTicks, Period: req.Period,
		WarnBps: warn, Action: req.Action, CreatedBy: userID})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a policy already exists for this scope")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to create budget policy")
		}
		return
	}
	h.publish(protocol.EventBudgetUpdated, uuidToString(workspaceID), "member", uuidToString(userID), map[string]any{"workspace_id": uuidToString(workspaceID)})
	writeJSON(w, http.StatusCreated, budgetPolicyToResponse(policy))
}

func (h *Handler) UpdateBudgetPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.budgetWorkspace(w, r, true)
	if !ok {
		return
	}
	policyID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req updateBudgetPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Revision < 1 || !validBudgetSettings(req.LimitUSDTicks, req.Period, req.WarnBPS, req.Action) {
		writeError(w, http.StatusBadRequest, "invalid budget settings")
		return
	}
	policy, err := h.Queries.UpdateBudgetPolicy(r.Context(), db.UpdateBudgetPolicyParams{ID: policyID, WorkspaceID: workspaceID,
		LimitUsdTicks: req.LimitUSDTicks, Period: req.Period, WarnBps: req.WarnBPS, Action: req.Action, Revision: req.Revision})
	if errors.Is(err, pgx.ErrNoRows) {
		current, getErr := h.Queries.GetBudgetPolicyInWorkspace(r.Context(), db.GetBudgetPolicyInWorkspaceParams{ID: policyID, WorkspaceID: workspaceID})
		if getErr == nil {
			writeRevisionConflict(w, "budget_policy", policyID, req.Revision, current.Revision)
		} else {
			writeError(w, http.StatusNotFound, "budget policy not found")
		}
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update budget policy")
		return
	}
	h.publish(protocol.EventBudgetUpdated, uuidToString(workspaceID), "member", uuidToString(userID), map[string]any{"workspace_id": uuidToString(workspaceID)})
	writeJSON(w, http.StatusOK, budgetPolicyToResponse(policy))
}

func (h *Handler) DeleteBudgetPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.budgetWorkspace(w, r, true)
	if !ok {
		return
	}
	policyID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete budget policy")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if err = q.DeleteBudgetOverridesForPolicy(r.Context(), policyID); err == nil {
		err = q.DeleteBudgetReservationsForPolicy(r.Context(), policyID)
	}
	if err == nil {
		err = q.DeleteBudgetPeriodsForPolicy(r.Context(), policyID)
	}
	if err == nil {
		_, err = q.DeleteBudgetPolicy(r.Context(), db.DeleteBudgetPolicyParams{ID: policyID, WorkspaceID: workspaceID})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "budget policy not found")
		return
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete budget policy")
		return
	}
	h.publish(protocol.EventBudgetUpdated, uuidToString(workspaceID), "member", uuidToString(userID), map[string]any{"workspace_id": uuidToString(workspaceID)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateBudgetOverride(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.budgetWorkspace(w, r, true)
	if !ok {
		return
	}
	policyID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req struct {
		Reason        string `json:"reason"`
		DurationHours int    `json:"duration_hours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.DurationHours == 0 {
		req.DurationHours = 24
	}
	if len(req.Reason) == 0 || len(req.Reason) > 256 || req.DurationHours < 1 || req.DurationHours > 24 {
		writeError(w, http.StatusBadRequest, "override reason and duration are invalid")
		return
	}
	if _, err := h.Queries.GetBudgetPolicyInWorkspace(r.Context(), db.GetBudgetPolicyInWorkspaceParams{ID: policyID, WorkspaceID: workspaceID}); err != nil {
		writeError(w, http.StatusNotFound, "budget policy not found")
		return
	}
	override, err := h.Queries.CreateBudgetOverride(r.Context(), db.CreateBudgetOverrideParams{ID: dbid.NewV7(), WorkspaceID: workspaceID,
		PolicyID: policyID, GrantedBy: userID, Reason: req.Reason, ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(time.Duration(req.DurationHours) * time.Hour), Valid: true}})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create budget override")
		return
	}
	h.publish(protocol.EventBudgetUpdated, uuidToString(workspaceID), "member", uuidToString(userID), map[string]any{"workspace_id": uuidToString(workspaceID)})
	writeJSON(w, http.StatusCreated, map[string]any{"id": uuidToString(override.ID), "policy_id": uuidToString(override.PolicyID), "reason": override.Reason, "expires_at": timestampToString(override.ExpiresAt)})
}
