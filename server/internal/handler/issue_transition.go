package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Transition rules and approval gates (F28).
//
// The gate is the seventh status guard, next to plan verification, the review
// gate, acceptance criteria, business rules, mirrors and the trust dial. Every
// status-change entry point that runs those runs this one too, or the rule is
// bypassable — enforced by TestIssueTransitionGateCoversEveryStatusWriter.
//
// The decision and the hold live in internal/service (issue_transition_gate.go)
// so the HTTP handlers and the native agent runtime share one implementation;
// this file keeps only the request-shaped plumbing.

const (
	// ErrCodeTransitionNotAllowed: no rule grants this actor the move.
	ErrCodeTransitionNotAllowed = "transition_not_allowed"
	// ErrCodeTransitionPending: this issue already has a request waiting.
	ErrCodeTransitionPending = "transition_pending"
	// ErrCodeAlreadyDecided: someone else approved or rejected first.
	ErrCodeAlreadyDecided = "already_decided"

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

// loadActorSquads fills in the actor's squad memberships (service gate).
func (h *Handler) loadActorSquads(ctx context.Context, workspaceID pgtype.UUID, actor *issuestatus.TransitionActor) {
	service.LoadTransitionActorSquads(ctx, h.Queries, workspaceID, actor)
}

// loadTransitionRulesForTarget reads the enabled rules that could govern a
// move into toCategory on an issue in projectID, with their grants.
func (h *Handler) loadTransitionRulesForTarget(ctx context.Context, workspaceID, projectID pgtype.UUID, toCategory string) ([]issuestatus.TransitionRule, error) {
	return service.TransitionRulesForTarget(ctx, h.Queries, workspaceID, projectID, toCategory)
}

// attachRuleActors loads the nominative grants for the given rule rows.
func (h *Handler) attachRuleActors(ctx context.Context, rows []db.IssueTransitionRule) ([]issuestatus.TransitionRule, error) {
	return service.AttachTransitionRuleActors(ctx, h.Queries, rows)
}

// decideTransition is the whole gate, minus the HTTP response. `fromStatus` is
// empty on a create.
func (h *Handler) decideTransition(
	r *http.Request, workspaceID, projectID pgtype.UUID, fromStatus, toStatus string,
) (issuestatus.TransitionDecision, error) {
	return service.DecideIssueTransition(r.Context(), h.Queries, workspaceID, projectID, fromStatus, toStatus, h.transitionActor(r, workspaceID))
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

// holdTransitionForApproval files the pending request (service gate: request
// row, audit entry, approver inbox) and answers 202.
func (h *Handler) holdTransitionForApproval(
	w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string, decision issuestatus.TransitionDecision,
) {
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	result, err := service.FileIssueTransitionRequest(r.Context(), h.Queries, h.Bus, issue, statusKey, actorType, actorID, decision)
	if err != nil {
		if errors.Is(err, service.ErrTransitionPending) && result.Existing != nil {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":      "this issue already has a status change waiting for approval",
				"code":       ErrCodeTransitionPending,
				"request_id": uuidToString(result.Existing.ID),
				"to":         result.Existing.ToStatus,
			})
			return
		}
		slog.Warn("transition gate: create request failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to request approval for this transition")
		return
	}

	resp := issueToResponse(issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":     "pending_approval",
		"request_id": uuidToString(result.Request.ID),
		"issue":      resp,
	})
}
