package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/goalstate"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Inline approvals (OS plan, chantier 3). Everything a human is asked to
// decide, in one feed, so it can be answered where the person already is:
// the issue's timeline, the inbox row, the chat panel, the phone, a chat
// channel. Three sources feed it — Decision Cards (which approval gates,
// plan gates, previews, watchdog verdicts and the rest all ride), held
// status transitions, and goal-loop questions — each keeping its own
// settle endpoint. The feed adds what those endpoints do not say: the kind
// of ask, the gate behind a card (type, arguments, paths, expiry), and
// whether the caller may decide it.

const (
	ApprovalSourceDecision     = "decision"
	ApprovalSourceTransition   = "transition"
	ApprovalSourceGoalQuestion = "goal_question"

	ApprovalKindDecision   = "decision"
	ApprovalKindGate       = "gate"
	ApprovalKindPlan       = "plan"
	ApprovalKindInterview  = "interview"
	ApprovalKindPreview    = "preview"
	ApprovalKindWatchdog   = "watchdog"
	ApprovalKindPipeline   = "pipeline"
	ApprovalKindGoalAttach = "goal_attach"
	ApprovalKindOrgAssign  = "org_assign"
	ApprovalKindTransition = "transition"
	ApprovalKindGoalAsk    = "goal_question"

	approvalsFeedCap      = 200
	approvalGateSweepSize = 100
)

// ApprovalIssueRef names the issue an ask belongs to.
type ApprovalIssueRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
}

// ApprovalActor is who asked or requested.
type ApprovalActor struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// ApprovalTransition is the held move behind a transition ask.
type ApprovalTransition struct {
	RequestID     string   `json:"request_id"`
	FromStatus    string   `json:"from_status"`
	ToStatus      string   `json:"to_status"`
	RuleID        *string  `json:"rule_id"`
	ApproverRoles []string `json:"approver_roles"`
}

// ApprovalItem is one pending ask.
type ApprovalItem struct {
	ID                  string                 `json:"id"`
	Source              string                 `json:"source"`
	Kind                string                 `json:"kind"`
	Issue               ApprovalIssueRef       `json:"issue"`
	TaskID              string                 `json:"task_id,omitempty"`
	AskedBy             ApprovalActor          `json:"asked_by"`
	Question            string                 `json:"question"`
	Options             []DecisionOption       `json:"options"`
	RecommendedOptionID string                 `json:"recommended_option_id,omitempty"`
	Urgency             string                 `json:"urgency"`
	CreatedAt           string                 `json:"created_at"`
	ExpiresAt           *string                `json:"expires_at"`
	SlaDeadlineAt       *string                `json:"sla_deadline_at"`
	CanDecide           bool                   `json:"can_decide"`
	CannotDecideReason  string                 `json:"cannot_decide_reason,omitempty"`
	Decision            *IssueDecisionResponse `json:"decision,omitempty"`
	Gate                *ApprovalGateResponse  `json:"gate,omitempty"`
	Transition          *ApprovalTransition    `json:"transition,omitempty"`
	GoalQuestion        *goalstate.Question    `json:"goal_question,omitempty"`
}

// ApprovalsResponse is the feed envelope. RunHalt rides along because a halt
// is the one thing that explains every refused ask at once.
type ApprovalsResponse struct {
	Approvals []ApprovalItem  `json:"approvals"`
	Total     int             `json:"total"`
	RunHalt   service.RunHalt `json:"run_halt"`
}

// decisionKind tells what a card stands for, from the rows that hang off it.
func (h *Handler) decisionKind(ctx context.Context, d db.IssueDecision) string {
	if _, err := h.Queries.GetApprovalGateByDecision(ctx, d.ID); err == nil {
		return ApprovalKindGate
	}
	if d.PlanVersion.Valid {
		return ApprovalKindPlan
	}
	if d.InterviewGroupID.Valid {
		return ApprovalKindInterview
	}
	if effects, err := h.Queries.ListAgentEffectsForDecision(ctx, d.ID); err == nil && len(effects) > 0 {
		return ApprovalKindPreview
	}
	if _, err := h.Queries.GetWatchdogVerdictByDecision(ctx, d.ID); err == nil {
		return ApprovalKindWatchdog
	}
	if _, err := h.Queries.GetPipelineRunByGateDecision(ctx, d.ID); err == nil {
		return ApprovalKindPipeline
	}
	var options []DecisionOption
	_ = json.Unmarshal(d.Options, &options)
	for _, o := range options {
		if strings.HasPrefix(o.ID, goalProposalOptionPrefix) {
			return ApprovalKindGoalAttach
		}
		if strings.HasPrefix(o.ID, orgAssignOptionPrefix) {
			return ApprovalKindOrgAssign
		}
	}
	return ApprovalKindDecision
}

// approvalFeedBuilder resolves the issues and actors an ask names, once each.
type approvalFeedBuilder struct {
	h      *Handler
	ctx    context.Context
	wsID   pgtype.UUID
	prefix string
	role   string
	gates  service.ApprovalGates
	issues map[string]db.Issue
	names  map[string]string
}

func (h *Handler) newApprovalFeedBuilder(ctx context.Context, wsID pgtype.UUID, role string) *approvalFeedBuilder {
	b := &approvalFeedBuilder{h: h, ctx: ctx, wsID: wsID, prefix: h.getIssuePrefix(ctx, wsID), role: role, gates: service.DefaultApprovalGates, issues: map[string]db.Issue{}, names: map[string]string{}}
	if ws, err := h.Queries.GetWorkspace(ctx, wsID); err == nil {
		b.gates = service.ApprovalGatesSettings(ws.Settings)
	}
	return b
}

func (b *approvalFeedBuilder) issue(id pgtype.UUID) (ApprovalIssueRef, bool) {
	key := uuidToString(id)
	issue, ok := b.issues[key]
	if !ok {
		row, err := b.h.Queries.GetIssue(b.ctx, id)
		if err != nil || row.WorkspaceID != b.wsID {
			return ApprovalIssueRef{}, false
		}
		issue = row
		b.issues[key] = row
	}
	return ApprovalIssueRef{ID: key, Identifier: b.prefix + "-" + strconv.Itoa(int(issue.Number)), Title: issue.Title, Status: issue.Status}, true
}

func (b *approvalFeedBuilder) actor(actorType string, id pgtype.UUID) ApprovalActor {
	out := ApprovalActor{Type: actorType, ID: uuidToString(id)}
	if !id.Valid {
		return out
	}
	key := actorType + ":" + out.ID
	if name, ok := b.names[key]; ok {
		out.Name = name
		return out
	}
	switch actorType {
	case "agent":
		if agent, err := b.h.Queries.GetAgent(b.ctx, id); err == nil {
			out.Name = agent.Name
		}
	case "member":
		if user, err := b.h.Queries.GetUser(b.ctx, id); err == nil {
			out.Name = user.Name
		}
	}
	b.names[key] = out.Name
	return out
}

func (b *approvalFeedBuilder) fromDecision(d db.IssueDecision, userID string) (ApprovalItem, bool) {
	ref, ok := b.issue(d.IssueID)
	if !ok {
		return ApprovalItem{}, false
	}
	resp := issueDecisionToResponse(d)
	resp.Learned = b.h.decisionHint(b.ctx, b.wsID, userID, d)
	item := ApprovalItem{
		ID: resp.ID, Source: ApprovalSourceDecision, Kind: b.h.decisionKind(b.ctx, d), Issue: ref, TaskID: resp.TaskID,
		AskedBy: b.actor(d.AskedByType, d.AskedByID), Question: d.Question, Options: resp.Options, RecommendedOptionID: resp.RecommendedOptionID,
		Urgency: d.Urgency, CreatedAt: resp.CreatedAt, SlaDeadlineAt: resp.SlaDeadlineAt, CanDecide: true, Decision: &resp,
	}
	if item.Kind == ApprovalKindGate {
		if gate, err := b.h.Queries.GetApprovalGateByDecision(b.ctx, d.ID); err == nil {
			gate = b.h.expireGate(b.ctx, gate)
			g := gateToResponse(gate)
			item.Gate = &g
			item.ExpiresAt = g.ExpiresAt
			if g.Status != "pending" {
				// Settled or expired underneath an unanswered card: not an ask any more.
				return ApprovalItem{}, false
			}
		}
		if !b.gates.GateApproverAllowed(b.role) {
			item.CanDecide = false
			item.CannotDecideReason = "gate_approvers_policy"
		}
	}
	return item, true
}

func (b *approvalFeedBuilder) fromTransition(req db.IssueTransitionRequest, actor issuestatus.TransitionActor) (ApprovalItem, bool) {
	ref, ok := b.issue(req.IssueID)
	if !ok {
		return ApprovalItem{}, false
	}
	var rule *issuestatus.TransitionRule
	if req.RuleID.Valid {
		if row, err := b.h.Queries.GetIssueTransitionRule(b.ctx, db.GetIssueTransitionRuleParams{ID: req.RuleID, WorkspaceID: req.WorkspaceID}); err == nil {
			if rules, err := b.h.attachRuleActors(b.ctx, []db.IssueTransitionRule{row}); err == nil && len(rules) > 0 {
				rule = &rules[0]
			}
		}
	}
	item := ApprovalItem{
		ID: uuidToString(req.ID), Source: ApprovalSourceTransition, Kind: ApprovalKindTransition, Issue: ref,
		AskedBy: b.actor(req.RequestedByType, req.RequestedByID), Question: "Move " + ref.Identifier + " from " + req.FromStatus + " to " + req.ToStatus,
		Options: []DecisionOption{{ID: "approve", Label: "Approve", Impact: "the issue moves to " + req.ToStatus}, {ID: "reject", Label: "Reject", Impact: "the issue stays where it is"}},
		Urgency: "normal", CreatedAt: timestampToString(req.CreatedAt), CanDecide: issuestatus.CanApprove(rule, actor),
		Transition: &ApprovalTransition{RequestID: uuidToString(req.ID), FromStatus: req.FromStatus, ToStatus: req.ToStatus, RuleID: uuidToPtr(req.RuleID), ApproverRoles: issuestatus.ApproverRolesFor(rule)},
	}
	if rule != nil && rule.RejectStatusKey != "" {
		item.Options[1].Impact = "the issue goes to " + rule.RejectStatusKey
	}
	if !item.CanDecide {
		item.CannotDecideReason = "not_an_approver"
	}
	return item, true
}

func (b *approvalFeedBuilder) fromGoal(goal db.IssueGoal) (ApprovalItem, bool) {
	q := service.GoalQuestionOf(goal.Question)
	if q == nil || q.Answer != "" {
		return ApprovalItem{}, false
	}
	ref, ok := b.issue(goal.IssueID)
	if !ok {
		return ApprovalItem{}, false
	}
	options := make([]DecisionOption, 0, len(q.Options))
	for i, o := range q.Options {
		options = append(options, DecisionOption{ID: strconv.Itoa(i), Label: o})
	}
	asked := ApprovalActor{Type: "agent"}
	if runID, ok := decisionUUID(q.RunID); ok {
		if task, terr := b.h.Queries.GetAgentTask(b.ctx, runID); terr == nil {
			asked = b.actor("agent", task.AgentID)
		}
	}
	return ApprovalItem{
		ID: uuidToString(goal.ID), Source: ApprovalSourceGoalQuestion, Kind: ApprovalKindGoalAsk, Issue: ref, TaskID: q.RunID,
		AskedBy: asked, Question: q.Prompt, Options: options, Urgency: "normal", CreatedAt: q.AskedAt, CanDecide: true, GoalQuestion: q,
	}, true
}

// ListApprovals: GET /api/approvals?issue_id= — the pending asks of the
// workspace, or of one issue. Member-visible: an ask the caller may not
// decide still shows, marked so, because "who can settle this" is part of
// the answer to "why is this run waiting".
func (h *Handler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	ctx := r.Context()
	b := h.newApprovalFeedBuilder(ctx, wsUUID, member.Role)
	actor := h.transitionActor(r, wsUUID)
	items := make([]ApprovalItem, 0)

	issueParam := strings.TrimSpace(r.URL.Query().Get("issue_id"))
	if issueParam != "" {
		issue, ok := h.loadIssueForUser(w, r, issueParam)
		if !ok {
			return
		}
		decisions, err := h.Queries.ListIssueDecisions(ctx, db.ListIssueDecisionsParams{IssueID: issue.ID, WorkspaceID: wsUUID})
		if err != nil {
			slog.Warn("approvals: list issue decisions failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load approvals")
			return
		}
		for _, d := range decisions {
			if len(d.Response) > 0 {
				continue
			}
			if item, ok := b.fromDecision(d, userID); ok {
				items = append(items, item)
			}
		}
		if req, err := h.Queries.GetPendingIssueTransitionRequestForIssue(ctx, db.GetPendingIssueTransitionRequestForIssueParams{WorkspaceID: wsUUID, IssueID: issue.ID}); err == nil {
			if item, ok := b.fromTransition(req, actor); ok {
				items = append(items, item)
			}
		}
		if goal, err := h.Queries.GetIssueGoal(ctx, db.GetIssueGoalParams{IssueID: issue.ID, WorkspaceID: wsUUID}); err == nil && goal.Status == "waiting_user" {
			if item, ok := b.fromGoal(goal); ok {
				items = append(items, item)
			}
		}
	} else {
		decisions, err := h.Queries.ListPendingIssueDecisionsForWorkspace(ctx, db.ListPendingIssueDecisionsForWorkspaceParams{WorkspaceID: wsUUID, Limit: approvalsFeedCap})
		if err != nil {
			slog.Warn("approvals: list pending decisions failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load approvals")
			return
		}
		for _, d := range decisions {
			if item, ok := b.fromDecision(d, userID); ok {
				items = append(items, item)
			}
		}
		if reqs, err := h.Queries.ListPendingIssueTransitionRequestsForWorkspace(ctx, wsUUID); err == nil {
			for _, req := range reqs {
				if item, ok := b.fromTransition(req, actor); ok {
					items = append(items, item)
				}
			}
		}
		if goals, err := h.Queries.ListWaitingIssueGoalsForWorkspace(ctx, wsUUID); err == nil {
			for _, goal := range goals {
				if item, ok := b.fromGoal(goal); ok {
					items = append(items, item)
				}
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	total := len(items)
	if len(items) > approvalsFeedCap {
		items = items[:approvalsFeedCap]
	}
	resp := ApprovalsResponse{Approvals: items, Total: total}
	if ws, err := h.Queries.GetWorkspace(ctx, wsUUID); err == nil {
		resp.RunHalt = service.RunHaltFromSettings(ws.Settings)
	}
	writeJSON(w, http.StatusOK, resp)
}

// publishApproval announces an ask (approval:asked) or its settlement
// (approval:decided) to the workspace. Issue-scoped and small: clients
// refetch the feed for the issue or the workspace.
//
// A settlement is also the single choke point where a chat message carrying
// the ask's buttons has to be rewritten, so it happens here rather than in
// each of the three settle endpoints.
func (h *Handler) publishApproval(ctx context.Context, eventType, actorType, actorID string, wsID, issueID pgtype.UUID, source, id, kind, outcome string) {
	if eventType == protocol.EventApprovalDecided {
		h.settleApprovalMessages(ctx, wsID, issueID, source, id, outcome)
	}
	if h.Bus == nil {
		return
	}
	payload := map[string]any{"source": source, "id": id, "issue_id": uuidToString(issueID), "kind": kind}
	if outcome != "" {
		payload["outcome"] = outcome
	}
	h.publish(eventType, uuidToString(wsID), actorType, actorID, payload)
}

// approvalOutcomeSentence turns the machine outcome into the words a chat
// reader gets where the original buttons stood.
func approvalOutcomeSentence(outcome string) string {
	switch outcome {
	case "":
		return "answered"
	case "expired":
		return "expired without an answer"
	}
	return outcome
}

// ExpireOverdueGates settles every pending gate past its deadline: the gate
// is denied for timeout, its card answered by the server so it leaves the
// inboxes and timelines, and the settlement announced. Returns how many.
func (h *Handler) ExpireOverdueGates(ctx context.Context) int {
	gates, err := h.Queries.ListExpiredPendingApprovalGates(ctx, db.ListExpiredPendingApprovalGatesParams{ExpiresAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, Limit: approvalGateSweepSize})
	if err != nil {
		slog.Warn("approval gate sweeper: list failed", "error", err)
		return 0
	}
	n := 0
	for _, gate := range gates {
		updated := h.expireGate(ctx, gate)
		if !updated.ResolvedAction.Valid {
			continue
		}
		n++
		answer, _ := json.Marshal(DecisionAnswer{OptionID: gateDenyOptionID})
		if _, err := h.Queries.RespondIssueDecisionAsSystem(ctx, db.RespondIssueDecisionAsSystemParams{ID: gate.DecisionRequestID, Response: answer}); err != nil {
			slog.Warn("approval gate sweeper: card not answered", "gate_id", uuidToString(gate.ID), "error", err)
		}
		h.publishApproval(ctx, protocol.EventApprovalDecided, "system", "", gate.WorkspaceID, gate.IssueID, ApprovalSourceDecision, uuidToString(gate.DecisionRequestID), ApprovalKindGate, "expired")
		if h.Bus != nil {
			h.publish(protocol.EventIssueAuxChanged, uuidToString(gate.WorkspaceID), "system", "", map[string]any{"issue_id": uuidToString(gate.IssueID)})
		}
	}
	return n
}
