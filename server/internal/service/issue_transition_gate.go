package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The F28 transition gate, shared by every status writer that is not the
// platform itself. The HTTP handlers and the native agent runtime call the
// same decision + hold code, so picking a different entry point cannot bypass
// a workspace's rules — the property TestIssueTransitionGateCoversEveryStatusWriter
// keeps enumerated on the write side.
//
// A workspace with no rules behaves exactly as before: silence is permission.

// ErrTransitionPending is returned by FileIssueTransitionRequest when the
// issue already has a request waiting; Existing carries it.
var ErrTransitionPending = errors.New("transition request already pending")

// FileTransitionResult reports what FileIssueTransitionRequest did.
type FileTransitionResult struct {
	// Request is the freshly created approval request (ErrTransitionPending
	// nil).
	Request db.IssueTransitionRequest
	// Existing is the request that was already waiting, set only alongside
	// ErrTransitionPending.
	Existing *db.IssueTransitionRequest
}

// TransitionRulesForTarget reads the enabled rules that could govern a move
// into toCategory on an issue in projectID, with their nominative grants.
func TransitionRulesForTarget(ctx context.Context, q *db.Queries, workspaceID, projectID pgtype.UUID, toCategory string) ([]issuestatus.TransitionRule, error) {
	rows, err := q.ListEnabledIssueTransitionRulesForTarget(ctx, db.ListEnabledIssueTransitionRulesForTargetParams{
		WorkspaceID: workspaceID,
		ToCategory:  toCategory,
		ProjectID:   projectID,
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return AttachTransitionRuleActors(ctx, q, rows)
}

// AttachTransitionRuleActors loads the nominative grants for the given rule
// rows and returns the resolver's pure shape.
func AttachTransitionRuleActors(ctx context.Context, q *db.Queries, rows []db.IssueTransitionRule) ([]issuestatus.TransitionRule, error) {
	ids := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	actors, err := q.ListIssueTransitionRuleActors(ctx, ids)
	if err != nil {
		return nil, err
	}
	byRule := map[string][]issuestatus.TransitionRuleActor{}
	for _, a := range actors {
		key := util.UUIDToString(a.RuleID)
		byRule[key] = append(byRule[key], issuestatus.TransitionRuleActor{
			Type: a.ActorType, ID: util.UUIDToString(a.ActorID),
		})
	}
	out := make([]issuestatus.TransitionRule, 0, len(rows))
	for _, row := range rows {
		id := util.UUIDToString(row.ID)
		out = append(out, issuestatus.TransitionRule{
			ID:               id,
			ProjectID:        util.UUIDToString(row.ProjectID),
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
	return out, nil
}

// LoadTransitionActorSquads fills in the actor's squad memberships. Called
// only when a candidate rule actually mentions squads, so the common path
// costs nothing.
func LoadTransitionActorSquads(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, actor *issuestatus.TransitionActor) {
	actorUUID, err := util.ParseUUID(actor.ID)
	if err != nil {
		return
	}
	ids, err := q.ListSquadIDsForActor(ctx, db.ListSquadIDsForActorParams{
		WorkspaceID: workspaceID,
		MemberType:  actor.Type,
		MemberID:    actorUUID,
	})
	if err != nil {
		slog.Warn("transition gate: squad lookup failed", "error", err)
		return
	}
	for _, id := range ids {
		actor.SquadIDs = append(actor.SquadIDs, util.UUIDToString(id))
	}
}

// DecideIssueTransition is the whole gate, minus the transport. fromStatus is
// empty on a create. The actor is built by the caller: the HTTP layer reads
// it off the request, the native runtime passes the agent — never a member
// role, an agent has none of its own (see issuestatus.TransitionActor).
func DecideIssueTransition(
	ctx context.Context,
	q *db.Queries,
	workspaceID, projectID pgtype.UUID,
	fromStatus, toStatus string,
	actor issuestatus.TransitionActor,
) (issuestatus.TransitionDecision, error) {
	toCategory := issuestatus.Effective(ctx, q, workspaceID, toStatus)
	if toCategory == "" {
		return issuestatus.TransitionDecision{Outcome: issuestatus.TransitionAllow}, nil
	}
	rules, err := TransitionRulesForTarget(ctx, q, workspaceID, projectID, toCategory)
	if err != nil {
		return issuestatus.TransitionDecision{}, err
	}
	if len(rules) == 0 {
		return issuestatus.TransitionDecision{Outcome: issuestatus.TransitionAllow}, nil
	}
	fromCategory := ""
	if fromStatus != "" {
		fromCategory = issuestatus.Effective(ctx, q, workspaceID, fromStatus)
	}
	if TransitionRulesNeedSquads(rules) {
		LoadTransitionActorSquads(ctx, q, workspaceID, &actor)
	}
	return issuestatus.Decide(rules, actor, fromCategory, toCategory, util.UUIDToString(projectID)), nil
}

// TransitionRulesNeedSquads reports whether any candidate rule can be decided
// by squad membership.
func TransitionRulesNeedSquads(rules []issuestatus.TransitionRule) bool {
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

// FileIssueTransitionRequest files the pending approval request, writes the
// audit entry, and notifies every member holding an approver role (inbox
// item + realtime events). Shared by the HTTP hold path and the native agent
// runtime so a held request behaves identically regardless of who asked.
//
// A nil bus skips the realtime events — the rows are the source of truth and
// every surface refetches; only open clients wait for the nudge.
func FileIssueTransitionRequest(
	ctx context.Context,
	q *db.Queries,
	bus *events.Bus,
	issue db.Issue,
	toStatus string,
	actorType, actorID string,
	decision issuestatus.TransitionDecision,
) (FileTransitionResult, error) {
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		return FileTransitionResult{}, errors.New("could not identify the requesting actor")
	}
	ruleID := pgtype.UUID{}
	if decision.Rule != nil {
		if parsed, perr := util.ParseUUID(decision.Rule.ID); perr == nil {
			ruleID = parsed
		}
	}

	req, err := q.CreateIssueTransitionRequest(ctx, db.CreateIssueTransitionRequestParams{
		ID:              dbid.NewV7(),
		WorkspaceID:     issue.WorkspaceID,
		IssueID:         issue.ID,
		FromStatus:      issue.Status,
		ToStatus:        toStatus,
		RuleID:          ruleID,
		RequestedByType: actorType,
		RequestedByID:   actorUUID,
	})
	if err != nil {
		// The unique partial index is the real enforcement; this is its
		// friendly half. Report the request already waiting so the caller can
		// point at it rather than filing a duplicate.
		if existing, getErr := q.GetPendingIssueTransitionRequestForIssue(ctx, db.GetPendingIssueTransitionRequestForIssueParams{
			WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		}); getErr == nil {
			return FileTransitionResult{Existing: &existing}, ErrTransitionPending
		}
		return FileTransitionResult{}, fmt.Errorf("create transition request: %w", err)
	}

	if bus != nil {
		bus.Publish(events.Event{
			Type: protocol.EventApprovalAsked, WorkspaceID: util.UUIDToString(issue.WorkspaceID), ActorType: actorType, ActorID: actorID,
			Payload: map[string]any{"source": "transition", "id": util.UUIDToString(req.ID), "issue_id": util.UUIDToString(issue.ID), "kind": "transition"},
		})
	}

	// Audit entry — same shape the handler's h.audit writes for the HTTP path.
	details, _ := json.Marshal(map[string]any{"request_id": util.UUIDToString(req.ID), "from": issue.Status, "to": toStatus})
	if _, err := q.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		WorkspaceID: issue.WorkspaceID,
		ActorType:   actorType,
		ActorID:     actorUUID,
		Action:      "issue.transition_requested",
		EntityType:  "issue",
		EntityID:    issue.ID,
		Details:     details,
	}); err != nil {
		slog.Warn("transition gate: audit write failed", "error", err)
	}

	notifyTransitionApprovers(ctx, q, bus, issue, req, decision.Rule)
	if bus != nil {
		bus.Publish(events.Event{
			Type:        protocol.EventIssueTransitionRequested,
			WorkspaceID: util.UUIDToString(issue.WorkspaceID),
			ActorType:   actorType,
			ActorID:     actorID,
			Payload: map[string]any{
				"request_id": util.UUIDToString(req.ID),
				"issue_id":   util.UUIDToString(issue.ID),
				"from":       issue.Status,
				"to":         toStatus,
			},
		})
	}
	return FileTransitionResult{Request: req}, nil
}

// notifyTransitionApprovers files one inbox item per member holding a role the
// rule accepts as an approver. Best-effort: the request is already committed,
// and refusing the caller because a notification failed would undo nothing.
func notifyTransitionApprovers(ctx context.Context, q *db.Queries, bus *events.Bus, issue db.Issue, req db.IssueTransitionRequest, rule *issuestatus.TransitionRule) {
	roles := issuestatus.ApproverRolesFor(rule)
	members, err := q.ListMembers(ctx, issue.WorkspaceID)
	if err != nil {
		slog.Warn("transition gate: approver lookup failed", "error", err)
		return
	}
	itemDetails, _ := json.Marshal(map[string]any{
		"request_id": util.UUIDToString(req.ID),
		"from":       req.FromStatus,
		"to":         req.ToStatus,
	})
	title := "Approval needed: " + issue.Title
	if len(title) > 100+len("…") {
		title = title[:100] + "…"
	}
	for _, member := range members {
		if !memberRoleAllowed(member.Role, roles) {
			continue
		}
		item, err := q.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   issue.WorkspaceID,
			RecipientType: "member",
			RecipientID:   member.UserID,
			Type:          "transition_approval_requested",
			Severity:      "action_required",
			IssueID:       issue.ID,
			Title:         title,
			Body:          pgtype.Text{String: req.FromStatus + " → " + req.ToStatus, Valid: true},
			Details:       itemDetails,
		})
		if err != nil {
			slog.Warn("transition gate: inbox failed", "error", err)
			continue
		}
		if bus != nil {
			bus.Publish(events.Event{
				Type:        protocol.EventInboxNew,
				WorkspaceID: util.UUIDToString(issue.WorkspaceID),
				ActorType:   "system",
				Payload:     map[string]any{"item": InboxItemPayload(item)},
			})
		}
	}
}

// memberRoleAllowed mirrors handler.roleAllowed for the roles a transition
// rule accepts as approvers: plain equality, no hierarchy — an owner is not
// an approver unless the rule names owners.
func memberRoleAllowed(role string, roles []string) bool {
	for _, r := range roles {
		if role == r {
			return true
		}
	}
	return false
}
