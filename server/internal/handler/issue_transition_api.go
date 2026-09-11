package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"

	"log/slog"
)

// Transition rules API (F28).
//
// Reading the rules is open to any workspace member — the status picker needs
// them to explain a greyed-out option. Writing them is owner/admin only, the
// same boundary as the status catalog they are written against.

type IssueTransitionRuleResponse struct {
	ID               string                             `json:"id"`
	WorkspaceID      string                             `json:"workspace_id"`
	ProjectID        *string                            `json:"project_id"`
	FromCategory     *string                            `json:"from_category"`
	ToCategory       string                             `json:"to_category"`
	AllowedRoles     []string                           `json:"allowed_roles"`
	AllowActorTypes  []string                           `json:"allow_actor_types"`
	RequiresApproval bool                               `json:"requires_approval"`
	ApproverRoles    []string                           `json:"approver_roles"`
	RejectStatusKey  *string                            `json:"reject_status_key"`
	Enabled          bool                               `json:"enabled"`
	Actors           []IssueTransitionRuleActorResponse `json:"actors"`
	CreatedAt        string                             `json:"created_at"`
	UpdatedAt        string                             `json:"updated_at"`
}

type IssueTransitionRuleActorResponse struct {
	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
}

type IssueTransitionRequestResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	IssueID         string  `json:"issue_id"`
	FromStatus      string  `json:"from_status"`
	ToStatus        string  `json:"to_status"`
	RuleID          *string `json:"rule_id"`
	RequestedByType string  `json:"requested_by_type"`
	RequestedByID   string  `json:"requested_by_id"`
	State           string  `json:"state"`
	DecidedByType   *string `json:"decided_by_type"`
	DecidedByID     *string `json:"decided_by_id"`
	DecidedAt       *string `json:"decided_at"`
	Note            *string `json:"note"`
	CreatedAt       string  `json:"created_at"`
}

func issueTransitionRuleToResponse(rule issuestatus.TransitionRule, row db.IssueTransitionRule) IssueTransitionRuleResponse {
	actors := make([]IssueTransitionRuleActorResponse, 0, len(rule.Actors))
	for _, a := range rule.Actors {
		actors = append(actors, IssueTransitionRuleActorResponse{ActorType: a.Type, ActorID: a.ID})
	}
	return IssueTransitionRuleResponse{
		ID:               rule.ID,
		WorkspaceID:      uuidToString(row.WorkspaceID),
		ProjectID:        uuidToPtr(row.ProjectID),
		FromCategory:     textToPtr(row.FromCategory),
		ToCategory:       rule.ToCategory,
		AllowedRoles:     nonNilStrings(rule.AllowedRoles),
		AllowActorTypes:  nonNilStrings(rule.AllowActorTypes),
		RequiresApproval: rule.RequiresApproval,
		ApproverRoles:    nonNilStrings(rule.ApproverRoles),
		RejectStatusKey:  textToPtr(row.RejectStatusKey),
		Enabled:          row.Enabled,
		Actors:           actors,
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
}

func issueTransitionRequestToResponse(req db.IssueTransitionRequest) IssueTransitionRequestResponse {
	return IssueTransitionRequestResponse{
		ID:              uuidToString(req.ID),
		WorkspaceID:     uuidToString(req.WorkspaceID),
		IssueID:         uuidToString(req.IssueID),
		FromStatus:      req.FromStatus,
		ToStatus:        req.ToStatus,
		RuleID:          uuidToPtr(req.RuleID),
		RequestedByType: req.RequestedByType,
		RequestedByID:   uuidToString(req.RequestedByID),
		State:           req.State,
		DecidedByType:   textToPtr(req.DecidedByType),
		DecidedByID:     uuidToPtr(req.DecidedByID),
		DecidedAt:       timestampToPtr(req.DecidedAt),
		Note:            textToPtr(req.Note),
		CreatedAt:       timestampToString(req.CreatedAt),
	}
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// ListIssueTransitionRules returns every rule in the workspace. Any member.
func (h *Handler) ListIssueTransitionRules(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	rows, err := h.Queries.ListIssueTransitionRules(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ListIssueTransitionRules failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list transition rules")
		return
	}
	resp := []IssueTransitionRuleResponse{}
	if len(rows) > 0 {
		rules, err := h.attachRuleActors(r.Context(), rows)
		if err != nil {
			slog.Warn("ListIssueTransitionRules actors failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to list transition rules")
			return
		}
		for i := range rules {
			resp = append(resp, issueTransitionRuleToResponse(rules[i], rows[i]))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rules":      resp,
		"categories": issuestatus.Canonical(),
		"total":      len(resp),
	})
}

type IssueTransitionRuleWriteRequest struct {
	ProjectID        *string                            `json:"project_id"`
	FromCategory     *string                            `json:"from_category"`
	ToCategory       *string                            `json:"to_category"`
	AllowedRoles     []string                           `json:"allowed_roles"`
	AllowActorTypes  []string                           `json:"allow_actor_types"`
	RequiresApproval *bool                              `json:"requires_approval"`
	ApproverRoles    []string                           `json:"approver_roles"`
	RejectStatusKey  *string                            `json:"reject_status_key"`
	Enabled          *bool                              `json:"enabled"`
	Actors           []IssueTransitionRuleActorResponse `json:"actors"`
}

var validTransitionRoles = []string{"owner", "admin", "member"}

// validateTransitionRuleBody checks the caller-supplied halves of a rule and
// returns a message when one is wrong.
func (h *Handler) validateTransitionRuleBody(r *http.Request, wsUUID pgtype.UUID, req IssueTransitionRuleWriteRequest, toCategory string) string {
	if toCategory == "" || !issuestatus.IsCategory(toCategory) {
		return "to_category must be one of: " + strings.Join(issuestatus.Canonical(), ", ")
	}
	if req.FromCategory != nil && *req.FromCategory != "" && !issuestatus.IsCategory(*req.FromCategory) {
		return "from_category must be one of: " + strings.Join(issuestatus.Canonical(), ", ")
	}
	for _, role := range append(append([]string{}, req.AllowedRoles...), req.ApproverRoles...) {
		if !roleAllowed(role, validTransitionRoles...) {
			return "roles must be one of: " + strings.Join(validTransitionRoles, ", ")
		}
	}
	for _, t := range req.AllowActorTypes {
		if t != issuestatus.ActorMember && t != issuestatus.ActorAgent && t != issuestatus.ActorSquad {
			return "allow_actor_types must be one of: member, agent, squad"
		}
	}
	for _, a := range req.Actors {
		if a.ActorType != issuestatus.ActorMember && a.ActorType != issuestatus.ActorAgent && a.ActorType != issuestatus.ActorSquad {
			return "actor_type must be one of: member, agent, squad"
		}
		if _, err := util.ParseUUID(a.ActorID); err != nil {
			return "actor_id must be a UUID"
		}
	}
	if req.RejectStatusKey != nil && *req.RejectStatusKey != "" {
		if _, err := issuestatus.Resolve(r.Context(), h.Queries, wsUUID, *req.RejectStatusKey); err != nil {
			return "reject_status_key is not a status of this workspace"
		}
	}
	return ""
}

// CreateIssueTransitionRule adds a rule. Owner/admin only.
func (h *Handler) CreateIssueTransitionRule(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	var req IssueTransitionRuleWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	toCategory := ""
	if req.ToCategory != nil {
		toCategory = *req.ToCategory
	}
	if msg := h.validateTransitionRuleBody(r, wsUUID, req, toCategory); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	projectID := pgtype.UUID{}
	if req.ProjectID != nil && *req.ProjectID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: parsed, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found in this workspace")
			return
		}
		projectID = parsed
	}

	row, err := h.Queries.CreateIssueTransitionRule(r.Context(), db.CreateIssueTransitionRuleParams{
		ID:               dbid.NewV7(),
		WorkspaceID:      wsUUID,
		ProjectID:        projectID,
		FromCategory:     ptrToText(req.FromCategory),
		ToCategory:       toCategory,
		AllowedRoles:     nonNilStrings(req.AllowedRoles),
		AllowActorTypes:  nonNilStrings(req.AllowActorTypes),
		RequiresApproval: req.RequiresApproval != nil && *req.RequiresApproval,
		ApproverRoles:    nonNilStrings(req.ApproverRoles),
		RejectStatusKey:  ptrToText(req.RejectStatusKey),
		Enabled:          req.Enabled == nil || *req.Enabled,
		CreatedBy:        member.UserID,
	})
	if err != nil {
		slog.Warn("CreateIssueTransitionRule failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create the transition rule")
		return
	}
	h.replaceRuleActors(r, row.ID, req.Actors)
	h.writeRuleResponse(w, r, row, http.StatusCreated)
}

// UpdateIssueTransitionRule patches a rule. Owner/admin only.
//
// The nullable fields (project_id, from_category, reject_status_key) are
// REPLACED, not merged: an editor that drops a scope has to be able to say so,
// and there is no third state between "unchanged" and "cleared" a category
// picker could express.
func (h *Handler) UpdateIssueTransitionRule(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	ruleID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "rule id")
	if !ok {
		return
	}
	existing, err := h.Queries.GetIssueTransitionRule(r.Context(), db.GetIssueTransitionRuleParams{ID: ruleID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "transition rule not found")
		return
	}
	var req IssueTransitionRuleWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	toCategory := existing.ToCategory
	if req.ToCategory != nil {
		toCategory = *req.ToCategory
	}
	if msg := h.validateTransitionRuleBody(r, wsUUID, req, toCategory); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	projectID := pgtype.UUID{}
	if req.ProjectID != nil && *req.ProjectID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: parsed, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found in this workspace")
			return
		}
		projectID = parsed
	}
	row, err := h.Queries.UpdateIssueTransitionRule(r.Context(), db.UpdateIssueTransitionRuleParams{
		ID:               ruleID,
		WorkspaceID:      wsUUID,
		FromCategory:     ptrToText(req.FromCategory),
		ToCategory:       ptrToText(req.ToCategory),
		AllowedRoles:     req.AllowedRoles,
		AllowActorTypes:  req.AllowActorTypes,
		RequiresApproval: ptrToBoolValue(req.RequiresApproval),
		ApproverRoles:    req.ApproverRoles,
		RejectStatusKey:  ptrToText(req.RejectStatusKey),
		Enabled:          ptrToBoolValue(req.Enabled),
		ProjectID:        projectID,
	})
	if err != nil {
		slog.Warn("UpdateIssueTransitionRule failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update the transition rule")
		return
	}
	if req.Actors != nil {
		h.replaceRuleActors(r, ruleID, req.Actors)
	}
	h.writeRuleResponse(w, r, row, http.StatusOK)
}

// DeleteIssueTransitionRule removes a rule and its nominative grants.
func (h *Handler) DeleteIssueTransitionRule(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	ruleID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "rule id")
	if !ok {
		return
	}
	// Grants first: a rule row that vanished while its actor rows survived
	// would leave grants nothing can reach or clean up.
	if err := h.Queries.DeleteIssueTransitionRuleActors(r.Context(), ruleID); err != nil {
		slog.Warn("DeleteIssueTransitionRule actors failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	rows, err := h.Queries.DeleteIssueTransitionRule(r.Context(), db.DeleteIssueTransitionRuleParams{ID: ruleID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the transition rule")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "transition rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) replaceRuleActors(r *http.Request, ruleID pgtype.UUID, actors []IssueTransitionRuleActorResponse) {
	if err := h.Queries.DeleteIssueTransitionRuleActors(r.Context(), ruleID); err != nil {
		slog.Warn("transition rule: clear actors failed", "error", err)
		return
	}
	for _, a := range actors {
		actorUUID, err := util.ParseUUID(a.ActorID)
		if err != nil {
			continue
		}
		if err := h.Queries.AddIssueTransitionRuleActor(r.Context(), db.AddIssueTransitionRuleActorParams{
			ID: dbid.NewV7(), RuleID: ruleID, ActorType: a.ActorType, ActorID: actorUUID,
		}); err != nil {
			slog.Warn("transition rule: add actor failed", "error", err)
		}
	}
}

func (h *Handler) writeRuleResponse(w http.ResponseWriter, r *http.Request, row db.IssueTransitionRule, status int) {
	rules, err := h.attachRuleActors(r.Context(), []db.IssueTransitionRule{row})
	if err != nil || len(rules) == 0 {
		writeJSON(w, status, issueTransitionRuleToResponse(issuestatus.TransitionRule{
			ID: uuidToString(row.ID), ToCategory: row.ToCategory,
			AllowedRoles: row.AllowedRoles, AllowActorTypes: row.AllowActorTypes,
			RequiresApproval: row.RequiresApproval, ApproverRoles: row.ApproverRoles,
		}, row))
		return
	}
	writeJSON(w, status, issueTransitionRuleToResponse(rules[0], row))
}

// EffectiveIssueTransitions answers, for one issue, what each target category
// would do right now: allowed, needs approval, or refused and why. The status
// picker greys and badges its options from this.
func (h *Handler) EffectiveIssueTransitions(w http.ResponseWriter, r *http.Request) {
	issueID := strings.TrimSpace(r.URL.Query().Get("issue_id"))
	if issueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	ctx := r.Context()
	rows, err := h.Queries.ListEnabledIssueTransitionRulesForIssue(ctx, db.ListEnabledIssueTransitionRulesForIssueParams{
		WorkspaceID: issue.WorkspaceID,
		ProjectID:   issue.ProjectID,
	})
	if err != nil {
		slog.Warn("EffectiveIssueTransitions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to evaluate transition rules")
		return
	}
	var rules []issuestatus.TransitionRule
	if len(rows) > 0 {
		rules, err = h.attachRuleActors(ctx, rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to evaluate transition rules")
			return
		}
	}
	actor := h.transitionActor(r, issue.WorkspaceID)
	if service.TransitionRulesNeedSquads(rules) {
		h.loadActorSquads(ctx, issue.WorkspaceID, &actor)
	}
	from := issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, issue.Status)
	projectID := uuidToString(issue.ProjectID)

	out := make([]map[string]any, 0, len(issuestatus.Canonical()))
	for _, category := range issuestatus.Canonical() {
		decision := issuestatus.Decide(rules, actor, from, category, projectID)
		entry := map[string]any{
			"to_category":       category,
			"allowed":           decision.Outcome != issuestatus.TransitionDeny,
			"requires_approval": decision.Outcome == issuestatus.TransitionNeedsApproval,
			"reason":            decision.Reason,
		}
		if decision.Rule != nil {
			entry["rule_id"] = decision.Rule.ID
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":      uuidToString(issue.ID),
		"from_category": from,
		"transitions":   out,
	})
}

// ListIssueTransitionRequests returns this issue's request history.
func (h *Handler) ListIssueTransitionRequests(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListIssueTransitionRequestsForIssue(r.Context(), db.ListIssueTransitionRequestsForIssueParams{
		WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list transition requests")
		return
	}
	resp := make([]IssueTransitionRequestResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, issueTransitionRequestToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": resp, "total": len(resp)})
}

type transitionDecisionRequest struct {
	Note string `json:"note"`
}

// loadTransitionRequestForDecision resolves the request and the issue it holds,
// and checks that this caller may decide it.
func (h *Handler) loadTransitionRequestForDecision(w http.ResponseWriter, r *http.Request) (db.IssueTransitionRequest, db.Issue, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.IssueTransitionRequest{}, db.Issue{}, false
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return db.IssueTransitionRequest{}, db.Issue{}, false
	}
	reqID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "request id")
	if !ok {
		return db.IssueTransitionRequest{}, db.Issue{}, false
	}
	req, err := h.Queries.GetIssueTransitionRequest(r.Context(), db.GetIssueTransitionRequestParams{ID: reqID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "transition request not found")
		return db.IssueTransitionRequest{}, db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: req.IssueID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.IssueTransitionRequest{}, db.Issue{}, false
	}
	return req, issue, true
}

// ruleForRequest reloads the rule a request was held by, for the approver
// check and the reject fallback. A rule deleted since the request was filed
// leaves owner/admin as the only approvers, which is the safe default.
func (h *Handler) ruleForRequest(r *http.Request, req db.IssueTransitionRequest) *issuestatus.TransitionRule {
	return h.transitionRuleFor(r.Context(), req)
}

// ApproveIssueTransitionRequest applies the held move as the approver.
func (h *Handler) ApproveIssueTransitionRequest(w http.ResponseWriter, r *http.Request) {
	h.decideIssueTransitionRequest(w, r, "approved")
}

// RejectIssueTransitionRequest closes the request. When the rule names a
// reject_status_key the issue is sent there; otherwise it stays put.
func (h *Handler) RejectIssueTransitionRequest(w http.ResponseWriter, r *http.Request) {
	h.decideIssueTransitionRequest(w, r, "rejected")
}

func (h *Handler) decideIssueTransitionRequest(w http.ResponseWriter, r *http.Request, state string) {
	req, issue, ok := h.loadTransitionRequestForDecision(w, r)
	if !ok {
		return
	}
	var body transitionDecisionRequest
	_ = json.NewDecoder(r.Body).Decode(&body)

	rule := h.ruleForRequest(r, req)
	actor := h.transitionActor(r, issue.WorkspaceID)
	if !issuestatus.CanApprove(rule, actor) {
		writeErrorCode(w, http.StatusForbidden, ErrCodeTransitionNotAllowed,
			"you are not an approver for this transition")
		return
	}
	if _, err := util.ParseUUID(actor.ID); err != nil {
		writeError(w, http.StatusBadRequest, "could not identify the deciding actor")
		return
	}

	decided, applied, err := h.decideTransitionCore(r.Context(), issue, req, rule, actor, state, optionalString(body.Note),
		h.actingTaskID(r), h.issueTriggerWriteProbe(r, actor.Type, actor.ID, issue))
	if err != nil {
		var applyErr transitionApplyError
		var refusal transitionGateRefusal
		switch {
		case errors.As(err, &refusal):
			writeErrorCode(w, http.StatusConflict, refusal.code, refusal.msg)
		case errors.As(err, &applyErr):
			if writeIssueStatusRaceError(w, applyErr.err) {
				return
			}
			slog.Warn("apply decided transition failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "the decision was recorded but the status could not be applied")
		case errors.Is(err, pgx.ErrNoRows):
			writeErrorCode(w, http.StatusConflict, ErrCodeAlreadyDecided,
				"this transition request has already been decided")
		default:
			slog.Warn("decide transition request failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to decide this transition request")
		}
		return
	}

	resp := issueToResponse(applied, h.getIssuePrefix(r.Context(), applied.WorkspaceID))
	h.fillStatusCategory(r.Context(), applied.WorkspaceID, &resp)
	writeJSON(w, http.StatusOK, map[string]any{
		"request": issueTransitionRequestToResponse(decided),
		"issue":   resp,
	})
}

// transitionApplyError marks the one failure that happens AFTER the decision
// is already recorded: the status write. The caller has to answer differently
// for it, because retrying the decision would now report "already decided".
type transitionApplyError struct{ err error }

func (e transitionApplyError) Error() string { return e.err.Error() }
func (e transitionApplyError) Unwrap() error { return e.err }

// decideTransitionCore records an approver's verdict on a held move, applies
// the resulting status, and announces both. It is the path behind the HTTP
// endpoints AND behind an approve/reject button clicked in a chat channel, so
// a transition settled from Slack leaves exactly the records one settled from
// the web leaves.
func (h *Handler) decideTransitionCore(ctx context.Context, issue db.Issue, req db.IssueTransitionRequest, rule *issuestatus.TransitionRule, actor issuestatus.TransitionActor, state string, note *string, actingTaskID pgtype.UUID, probe service.IssueTriggerProbe) (db.IssueTransitionRequest, db.Issue, error) {
	actorUUID, err := util.ParseUUID(actor.ID)
	if err != nil {
		return db.IssueTransitionRequest{}, issue, err
	}
	// The request passed the state gates when it was filed, but an approval
	// can come much later: a criterion lost its proof, a mirror opened. Those
	// are re-checked before deciding, so a refusal leaves the request pending
	// rather than recording an approval that cannot be applied. Actor gates
	// (critic hold, trust dial, submit-review rules) judged the requester and
	// stay settled; the transition rule itself is what the approver decides.
	decidedTarget := ""
	switch state {
	case "approved":
		decidedTarget = req.ToStatus
	case "rejected":
		if rule != nil {
			decidedTarget = rule.RejectStatusKey
		}
	}
	if decidedTarget != "" && decidedTarget != issue.Status {
		if err := h.stateGatesAllowDecidedMove(ctx, issue, decidedTarget); err != nil {
			return db.IssueTransitionRequest{}, issue, err
		}
	}
	// The concurrency fence: two approvers racing both run this and only the
	// first matches a pending row.
	decided, err := h.Queries.DecideIssueTransitionRequest(ctx, db.DecideIssueTransitionRequestParams{
		ID:            req.ID,
		WorkspaceID:   req.WorkspaceID,
		State:         state,
		DecidedByType: pgtype.Text{String: actor.Type, Valid: true},
		DecidedByID:   actorUUID,
		Note:          ptrToText(note),
	})
	if err != nil {
		return db.IssueTransitionRequest{}, issue, err
	}

	targetStatus := ""
	switch state {
	case "approved":
		targetStatus = decided.ToStatus
	case "rejected":
		if rule != nil && rule.RejectStatusKey != "" {
			targetStatus = rule.RejectStatusKey
		}
	}
	applied := issue
	if targetStatus != "" && targetStatus != issue.Status {
		applied, err = h.applyDecidedTransition(ctx, issue, targetStatus, actor.Type, actor.ID, actingTaskID, probe)
		if err != nil {
			return decided, issue, transitionApplyError{err: err}
		}
	}

	h.audit(ctx, issue.WorkspaceID, actor.Type, actor.ID, AuditTransitionDecided, "issue", issue.ID,
		map[string]any{"request_id": uuidToString(decided.ID), "state": state, "to": targetStatus}, nil)
	event := protocol.EventIssueTransitionApproved
	if state == "rejected" {
		event = protocol.EventIssueTransitionRejected
	}
	h.publish(event, uuidToString(issue.WorkspaceID), actor.Type, actor.ID,
		map[string]any{"request": issueTransitionRequestToResponse(decided)})
	h.publishApproval(ctx, protocol.EventApprovalDecided, actor.Type, actor.ID, issue.WorkspaceID, issue.ID, ApprovalSourceTransition, uuidToString(decided.ID), ApprovalKindTransition, state)
	return decided, applied, nil
}

// transitionGateRefusal is a state gate refusing a decided move; the request
// stays pending. Code is the same error code the gate answers on UpdateIssue.
type transitionGateRefusal struct {
	code string
	msg  string
}

func (e transitionGateRefusal) Error() string { return e.msg }

// stateGatesAllowDecidedMove runs the issue-state gates UpdateIssue runs
// (plan verification, review gate, acceptance criteria, open mirrors) for a
// move an approver is deciding. A read failure is an error, never a pass.
func (h *Handler) stateGatesAllowDecidedMove(ctx context.Context, issue db.Issue, statusKey string) error {
	if blocked, err := h.planVerificationBlocksDone(ctx, issue, statusKey); err != nil {
		return err
	} else if blocked {
		return transitionGateRefusal{code: ErrCodePlanVerificationCritical, msg: "plan verification found a critical divergence; publish a new plan version or a clean verification before marking done"}
	}
	if reason, err := h.reviewGateBlocksDone(ctx, issue, statusKey); err != nil {
		return err
	} else if reason != "" {
		return transitionGateRefusal{msg: reason}
	}
	if issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, statusKey) == issuestatus.Done {
		if unsatisfied := unsatisfiedAcceptanceCriteria(issue.AcceptanceCriteria); len(unsatisfied) > 0 {
			return transitionGateRefusal{code: ErrCodeUnsatisfiedAcceptanceCriteria, msg: fmt.Sprintf("%d acceptance criteria lack proof", len(unsatisfied))}
		}
	}
	if _, identifiers, err := h.openMirrorsBlockingDone(ctx, issue, statusKey); err != nil {
		return err
	} else if len(identifiers) > 0 {
		return transitionGateRefusal{code: ErrCodeOpenMirrors, msg: openMirrorsMessage(identifiers)}
	}
	return nil
}

// transitionRuleFor is ruleForRequest without a request, for the chat-button
// path. A rule deleted since the request was filed leaves owner/admin as the
// only approvers, which is the safe default.
func (h *Handler) transitionRuleFor(ctx context.Context, req db.IssueTransitionRequest) *issuestatus.TransitionRule {
	if !req.RuleID.Valid {
		return nil
	}
	row, err := h.Queries.GetIssueTransitionRule(ctx, db.GetIssueTransitionRuleParams{ID: req.RuleID, WorkspaceID: req.WorkspaceID})
	if err != nil {
		return nil
	}
	rules, err := h.attachRuleActors(ctx, []db.IssueTransitionRule{row})
	if err != nil || len(rules) == 0 {
		return nil
	}
	return &rules[0]
}

// applyDecidedTransition writes the status through the same archive-race guard
// every other status write uses, then publishes issue:updated with
// status_changed and starts the run the original write would have started.
func (h *Handler) applyDecidedTransition(ctx context.Context, issue db.Issue, statusKey, actorType, actorID string, actingTaskID pgtype.UUID, probe service.IssueTriggerProbe) (db.Issue, error) {
	var updated db.Issue
	err := h.runWithIssueStatusGuard(ctx, issue.WorkspaceID, statusKey, func(q *db.Queries) error {
		var innerErr error
		updated, innerErr = q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID: issue.ID, Status: statusKey, WorkspaceID: issue.WorkspaceID,
		})
		return innerErr
	})
	if err != nil {
		return issue, err
	}

	prefix := h.getIssuePrefix(ctx, updated.WorkspaceID)
	resp := issueToResponse(updated, prefix)
	h.fillStatusCategory(ctx, updated.WorkspaceID, &resp)
	h.publish(protocol.EventIssueUpdated, uuidToString(updated.WorkspaceID), actorType, actorID, map[string]any{
		"issue":          resp,
		"acting_task_id": uuidToString(actingTaskID),
		"status_changed": true,
	})
	h.audit(ctx, updated.WorkspaceID, actorType, actorID, AuditIssueStatus, "issue", updated.ID,
		map[string]any{"from": issue.Status, "to": updated.Status}, nil)

	// The approved move is the write the requester attempted, so it starts the
	// run that write would have started — otherwise an approval gate would
	// silently turn "assign and start" into "assign".
	if trigger, ok := h.IssueService.WillEnqueueRun(ctx, service.IssueTriggerInput{
		Issue:         updated,
		PrevStatus:    issue.Status,
		StatusChanged: true,
	}, probe); ok {
		h.dispatchIssueRun(ctx, updated, trigger, actorType, actorID, "")
	}
	return updated, nil
}

// CancelIssueTransitionRequest withdraws one's own pending request.
func (h *Handler) CancelIssueTransitionRequest(w http.ResponseWriter, r *http.Request) {
	req, issue, ok := h.loadTransitionRequestForDecision(w, r)
	if !ok {
		return
	}
	actor := h.transitionActor(r, issue.WorkspaceID)
	if uuidToString(req.RequestedByID) != actor.ID || req.RequestedByType != actor.Type {
		writeErrorCode(w, http.StatusForbidden, ErrCodeTransitionNotAllowed,
			"only the requester can cancel this transition request")
		return
	}
	actorUUID, err := util.ParseUUID(actor.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not identify the cancelling actor")
		return
	}
	if _, err := h.Queries.DecideIssueTransitionRequest(r.Context(), db.DecideIssueTransitionRequestParams{
		ID: req.ID, WorkspaceID: req.WorkspaceID, State: "cancelled",
		DecidedByType: pgtype.Text{String: actor.Type, Valid: true}, DecidedByID: actorUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErrorCode(w, http.StatusConflict, ErrCodeAlreadyDecided,
				"this transition request has already been decided")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to cancel this transition request")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ptrToBoolValue mirrors ptrToText for the nullable-bool patch fields sqlc
// generates from sqlc.narg.
func ptrToBoolValue(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}

func optionalString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
