package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Transition rules and approval gates (F28).
//
// The gate is the seventh status guard, next to plan verification, the review
// gate, acceptance criteria, business rules, mirrors and the trust dial. Every
// status-change entry point that runs those runs this one too, or the rule is
// bypassable — enforced by TestIssueTransitionGateCoversEveryStatusWriter.
//
// A workspace with no rules pays one indexed lookup that returns zero rows and
// then behaves exactly as it did before: silence is permission.

const (
	// ErrCodeTransitionNotAllowed: no rule grants this actor the move.
	ErrCodeTransitionNotAllowed = "transition_not_allowed"
	// ErrCodeTransitionPending: this issue already has a request waiting.
	ErrCodeTransitionPending = "transition_pending"
	// ErrCodeAlreadyDecided: someone else approved or rejected first.
	ErrCodeAlreadyDecided = "already_decided"

	// InboxTypeTransitionApproval is filed for every member holding an
	// approver role when a transition is held.
	InboxTypeTransitionApproval = "transition_approval_requested"

	AuditTransitionRequested = "issue.transition_requested"
	AuditTransitionDecided   = "issue.transition_decided"
)

// transitionActor builds the resolver's view of who is making this request.
//
// The role is read from the workspace member row ONLY for a member actor. An
// agent's task token authenticates as its owning human, so reading the role
// for an agent would silently hand the agent its owner's authority — the one
// mistake this whole feature exists to prevent.
func (h *Handler) transitionActor(r *http.Request, workspaceID pgtype.UUID) issuestatus.TransitionActor {
	wsID := uuidToString(workspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsID)
	actor := issuestatus.TransitionActor{Type: actorType, ID: actorID}
	if actorType == issuestatus.ActorMember {
		if member, err := h.getWorkspaceMember(r.Context(), actorID, wsID); err == nil {
			actor.Role = member.Role
		}
	}
	return actor
}

// loadActorSquads fills in the actor's squad memberships. Called only when a
// candidate rule actually mentions squads, so the common path costs nothing.
func (h *Handler) loadActorSquads(ctx context.Context, workspaceID pgtype.UUID, actor *issuestatus.TransitionActor) {
	actorUUID, err := util.ParseUUID(actor.ID)
	if err != nil {
		return
	}
	ids, err := h.Queries.ListSquadIDsForActor(ctx, db.ListSquadIDsForActorParams{
		WorkspaceID: workspaceID,
		MemberType:  actor.Type,
		MemberID:    actorUUID,
	})
	if err != nil {
		slog.Warn("transition gate: squad lookup failed", "error", err)
		return
	}
	for _, id := range ids {
		actor.SquadIDs = append(actor.SquadIDs, uuidToString(id))
	}
}

// transitionRulesNeedSquads reports whether any candidate rule can be decided
// by squad membership.
func transitionRulesNeedSquads(rules []issuestatus.TransitionRule) bool {
	for _, rule := range rules {
		for _, t := range rule.AllowActorTypes {
			if t == issuestatus.ActorSquad {
				return true
			}
		}
		for _, a := range rule.Actors {
			if a.Type == issuestatus.ActorSquad {
				return true
			}
		}
	}
	return false
}

// toTransitionRules converts rows plus their nominative grants into the
// resolver's pure shape.
func toTransitionRules(rows []db.IssueTransitionRule, actors []db.IssueTransitionRuleActor) []issuestatus.TransitionRule {
	byRule := map[string][]issuestatus.TransitionRuleActor{}
	for _, a := range actors {
		key := uuidToString(a.RuleID)
		byRule[key] = append(byRule[key], issuestatus.TransitionRuleActor{
			Type: a.ActorType, ID: uuidToString(a.ActorID),
		})
	}
	out := make([]issuestatus.TransitionRule, 0, len(rows))
	for _, row := range rows {
		id := uuidToString(row.ID)
		out = append(out, issuestatus.TransitionRule{
			ID:               id,
			ProjectID:        uuidToString(row.ProjectID),
			FromCategory:     row.FromCategory.String,
			ToCategory:       row.ToCategory,
			AllowedRoles:     row.AllowedRoles,
			AllowActorTypes:  row.AllowActorTypes,
			RequiresApproval: row.RequiresApproval,
			ApproverRoles:    row.ApproverRoles,
			RejectStatusKey:  row.RejectStatusKey.String,
			Actors:           byRule[id],
		})
	}
	return out
}

// loadTransitionRulesForTarget reads the enabled rules that could govern a
// move into toCategory on an issue in projectID, with their grants.
func (h *Handler) loadTransitionRulesForTarget(ctx context.Context, workspaceID, projectID pgtype.UUID, toCategory string) ([]issuestatus.TransitionRule, error) {
	rows, err := h.Queries.ListEnabledIssueTransitionRulesForTarget(ctx, db.ListEnabledIssueTransitionRulesForTargetParams{
		WorkspaceID: workspaceID,
		ToCategory:  toCategory,
		ProjectID:   projectID,
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return h.attachRuleActors(ctx, rows)
}

func (h *Handler) attachRuleActors(ctx context.Context, rows []db.IssueTransitionRule) ([]issuestatus.TransitionRule, error) {
	ids := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	actors, err := h.Queries.ListIssueTransitionRuleActors(ctx, ids)
	if err != nil {
		return nil, err
	}
	return toTransitionRules(rows, actors), nil
}

// decideTransition is the whole gate, minus the HTTP response. `fromStatus` is
// empty on a create.
func (h *Handler) decideTransition(
	r *http.Request, workspaceID, projectID pgtype.UUID, fromStatus, toStatus string,
) (issuestatus.TransitionDecision, error) {
	ctx := r.Context()
	toCategory := issuestatus.Effective(ctx, h.Queries, workspaceID, toStatus)
	if toCategory == "" {
		return issuestatus.TransitionDecision{Outcome: issuestatus.TransitionAllow}, nil
	}
	rules, err := h.loadTransitionRulesForTarget(ctx, workspaceID, projectID, toCategory)
	if err != nil {
		return issuestatus.TransitionDecision{}, err
	}
	if len(rules) == 0 {
		return issuestatus.TransitionDecision{Outcome: issuestatus.TransitionAllow}, nil
	}
	fromCategory := ""
	if fromStatus != "" {
		fromCategory = issuestatus.Effective(ctx, h.Queries, workspaceID, fromStatus)
	}
	actor := h.transitionActor(r, workspaceID)
	if transitionRulesNeedSquads(rules) {
		h.loadActorSquads(ctx, workspaceID, &actor)
	}
	return issuestatus.Decide(rules, actor, fromCategory, toCategory, uuidToString(projectID)), nil
}

// transitionAllowsStatus is the gate as the status-write entry points use it:
// it returns false when it has already written the response, exactly like
// mirrorsAllowStatus and the other five guards next to it.
//
// A denial is 403 and the issue is untouched. An approval gate answers 202
// with the issue as it still is plus the request id — the write is HELD, so no
// run is enqueued and no autopilot finalizes, because the caller returns here.
func (h *Handler) transitionAllowsStatus(w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string) bool {
	if statusKey == "" || statusKey == issue.Status {
		return true
	}
	decision, err := h.decideTransition(r, issue.WorkspaceID, issue.ProjectID, issue.Status, statusKey)
	if err != nil {
		slog.Warn("transition gate failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to evaluate transition rules")
		return false
	}
	switch decision.Outcome {
	case issuestatus.TransitionAllow:
		return true
	case issuestatus.TransitionDeny:
		writeTransitionDenied(w, issue.Status, statusKey, decision)
		return false
	default:
		h.holdTransitionForApproval(w, r, issue, statusKey, decision)
		return false
	}
}

// transitionAllowsCreate gates a create landing on a non-default status.
//
// An ordinary create (the default `todo`) is never gated: a rule written
// against the todo category exists to govern MOVES back into the queue, and
// applying it to creation would stop a member filing a ticket at all.
//
// An approval gate cannot hold a create — there is no issue row to attach the
// request to — so NeedsApproval is answered as a refusal that says so. Create
// the issue in the default status and request the move.
func (h *Handler) transitionAllowsCreate(w http.ResponseWriter, r *http.Request, workspaceID, projectID pgtype.UUID, statusKey string) bool {
	if statusKey == "" {
		return true
	}
	if issuestatus.Effective(r.Context(), h.Queries, workspaceID, statusKey) == issuestatus.Todo {
		return true
	}
	decision, err := h.decideTransition(r, workspaceID, projectID, "", statusKey)
	if err != nil {
		slog.Warn("transition gate failed on create", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to evaluate transition rules")
		return false
	}
	if decision.Outcome == issuestatus.TransitionAllow {
		return true
	}
	writeTransitionDenied(w, "", statusKey, decision)
	return false
}

func writeTransitionDenied(w http.ResponseWriter, from, to string, decision issuestatus.TransitionDecision) {
	msg := "a transition rule does not allow you to move this issue to " + to
	if decision.Reason == issuestatus.ReasonRequiresApproval {
		msg = "moving to " + to + " needs approval, which cannot be requested while creating an issue; create it first, then request the move"
	}
	body := map[string]any{
		"error":  msg,
		"code":   ErrCodeTransitionNotAllowed,
		"from":   from,
		"to":     to,
		"reason": decision.Reason,
	}
	if decision.Rule != nil {
		body["rule_id"] = decision.Rule.ID
	}
	writeJSON(w, http.StatusForbidden, body)
}

// holdTransitionForApproval files the pending request and answers 202.
func (h *Handler) holdTransitionForApproval(
	w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string, decision issuestatus.TransitionDecision,
) {
	ctx := r.Context()
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not identify the requesting actor")
		return
	}
	ruleID := pgtype.UUID{}
	if decision.Rule != nil {
		if parsed, perr := util.ParseUUID(decision.Rule.ID); perr == nil {
			ruleID = parsed
		}
	}

	req, err := h.Queries.CreateIssueTransitionRequest(ctx, db.CreateIssueTransitionRequestParams{
		ID:              dbid.NewV7(),
		WorkspaceID:     issue.WorkspaceID,
		IssueID:         issue.ID,
		FromStatus:      issue.Status,
		ToStatus:        statusKey,
		RuleID:          ruleID,
		RequestedByType: actorType,
		RequestedByID:   actorUUID,
	})
	if err != nil {
		// The unique partial index is the real enforcement; this is its
		// friendly half. Report the request already waiting so the caller can
		// point at it rather than filing a duplicate.
		if existing, getErr := h.Queries.GetPendingIssueTransitionRequestForIssue(ctx, db.GetPendingIssueTransitionRequestForIssueParams{
			WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		}); getErr == nil {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":      "this issue already has a status change waiting for approval",
				"code":       ErrCodeTransitionPending,
				"request_id": uuidToString(existing.ID),
				"to":         existing.ToStatus,
			})
			return
		}
		slog.Warn("transition gate: create request failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to request approval for this transition")
		return
	}

	h.audit(ctx, issue.WorkspaceID, actorType, actorID, AuditTransitionRequested, "issue", issue.ID,
		map[string]any{"request_id": uuidToString(req.ID), "from": issue.Status, "to": statusKey}, nil)
	h.notifyTransitionApprovers(ctx, issue, req, decision.Rule)
	h.publish(protocol.EventIssueTransitionRequested, uuidToString(issue.WorkspaceID), actorType, actorID,
		map[string]any{"request": issueTransitionRequestToResponse(req)})

	resp := issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID))
	h.fillStatusCategory(ctx, issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":     "pending_approval",
		"request_id": uuidToString(req.ID),
		"issue":      resp,
	})
}

// notifyTransitionApprovers files one inbox item per member holding a role the
// rule accepts as an approver. Best-effort: the request is already committed,
// and refusing the response because a notification failed would undo nothing.
func (h *Handler) notifyTransitionApprovers(ctx context.Context, issue db.Issue, req db.IssueTransitionRequest, rule *issuestatus.TransitionRule) {
	roles := issuestatus.ApproverRolesFor(rule)
	members, err := h.Queries.ListMembers(ctx, issue.WorkspaceID)
	if err != nil {
		slog.Warn("transition gate: approver lookup failed", "error", err)
		return
	}
	details, _ := json.Marshal(map[string]any{
		"request_id": uuidToString(req.ID),
		"from":       req.FromStatus,
		"to":         req.ToStatus,
	})
	for _, member := range members {
		if !roleAllowed(member.Role, roles...) {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID,
			RecipientType: "member", RecipientID: member.UserID,
			Type: InboxTypeTransitionApproval, Severity: "action_required",
			IssueID: issue.ID,
			Title:   "Approval needed: " + truncate(issue.Title, 100),
			Body:    pgtype.Text{String: req.FromStatus + " → " + req.ToStatus, Valid: true},
			Details: details,
		})
		if err != nil {
			slog.Warn("transition gate: inbox failed", "error", err)
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(issue.WorkspaceID), "system", "",
			map[string]any{"item": inboxToResponse(item)})
	}
}
