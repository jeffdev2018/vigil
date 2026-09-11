package handler

import (
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
)

// Inbox zero (K63): the Decision Cards waiting for me, options included,
// ordered like the Attention Inbox (K02 risk) then by SLA deadline (K35),
// capped at five with the total, so a phone can answer them one tap each
// through the ordinary respond endpoint (K01). A projection: nothing is
// stored.
//
// JEF-244: ?include=transitions,goal_questions widens the scan to the other
// two "waiting on a human" sources the approvals feed models — held status
// moves and goal-loop questions — each resolved to the payload its settle
// endpoint expects. Without the param the projection is exactly the Decision
// Cards it always was. Unknown include values are ignored, so a newer client
// can ask for sources an older server does not know.

const inboxDecisionsCap = 5

// inboxRowTypeTransitionApproval is the inbox row a held status move files
// (service/issue_transition_gate.go); the goal-question row type already has
// a home: service.GoalInboxQuestionType.
const inboxRowTypeTransitionApproval = "transition_approval_requested"

type InboxDecisionItem struct {
	InboxItemID     string                 `json:"inbox_item_id"`
	IssueID         string                 `json:"issue_id"`
	IssueIdentifier string                 `json:"issue_identifier"`
	IssueTitle      string                 `json:"issue_title"`
	RiskScore       int                    `json:"risk_score"`
	Source          string                 `json:"source"`
	Decision        *IssueDecisionResponse `json:"decision,omitempty"`
	Transition      *ApprovalTransition    `json:"transition,omitempty"`
	GoalQuestion    *goalstate.Question    `json:"goal_question,omitempty"`

	// Sort keys, kept off the wire: only Decision Cards carry an SLA
	// deadline, so the shared ordering needs its own copies.
	sortDeadline *string
	sortCreated  string
}

// ListInboxDecisions: GET /api/inbox/decisions.
func (h *Handler) ListInboxDecisions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	include := map[string]bool{}
	for _, v := range strings.Split(r.URL.Query().Get("include"), ",") {
		include[strings.TrimSpace(v)] = true
	}
	wantTransitions := include["transitions"]
	wantGoalQuestions := include["goal_questions"]
	types := []string{"decision_request", "decision_escalated"}
	if wantTransitions {
		types = append(types, inboxRowTypeTransitionApproval)
	}
	if wantGoalQuestions {
		types = append(types, service.GoalInboxQuestionType)
	}
	rows, err := h.Queries.ListInboxDecisionSourceItems(r.Context(), db.ListInboxDecisionSourceItemsParams{WorkspaceID: wsUUID, RecipientType: "member", RecipientID: parseUUID(userID), Types: types})
	if err != nil {
		slog.Warn("list inbox decisions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list decisions")
		return
	}
	now := time.Now()
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	// A constant number of round trips for the whole page instead of one per
	// row: the inbox can hold 200 candidate rows for the five cards that get
	// shown. Transitions and goal questions resolve against the workspace's
	// pending sets — the same "pending" the approvals feed uses — so a settled
	// entity drops out even while its inbox row lingers.
	type candidate struct {
		row      db.ListInboxDecisionSourceItemsRow
		source   string
		entityID string
	}
	seen := map[string]bool{}
	candidates := make([]candidate, 0)
	decisionIDs := make([]pgtype.UUID, 0)
	issueIDs := make([]pgtype.UUID, 0)
	hasTransition := false
	hasGoalQuestion := false
	for _, row := range rows {
		var source, entityID string
		switch row.Type {
		case "decision_request", "decision_escalated":
			source = ApprovalSourceDecision
			var details struct {
				DecisionID string `json:"decision_id"`
			}
			if json.Unmarshal(row.Details, &details) != nil {
				continue
			}
			entityID = details.DecisionID
		case inboxRowTypeTransitionApproval:
			source = ApprovalSourceTransition
			var details struct {
				RequestID string `json:"request_id"`
			}
			if json.Unmarshal(row.Details, &details) != nil {
				continue
			}
			entityID = details.RequestID
		case service.GoalInboxQuestionType:
			source = ApprovalSourceGoalQuestion
			var details struct {
				GoalID string `json:"goal_id"`
			}
			if json.Unmarshal(row.Details, &details) != nil {
				continue
			}
			entityID = details.GoalID
		default:
			continue
		}
		if entityID == "" || seen[source+":"+entityID] {
			continue
		}
		did, ok := decisionUUID(entityID)
		if !ok {
			continue
		}
		seen[source+":"+entityID] = true
		candidates = append(candidates, candidate{row: row, source: source, entityID: entityID})
		if source == ApprovalSourceDecision {
			decisionIDs = append(decisionIDs, did)
		}
		hasTransition = hasTransition || source == ApprovalSourceTransition
		hasGoalQuestion = hasGoalQuestion || source == ApprovalSourceGoalQuestion
		issueIDs = append(issueIDs, row.IssueID)
	}
	decisions := map[string]db.IssueDecision{}
	transitions := map[string]db.IssueTransitionRequest{}
	goals := map[string]db.IssueGoal{}
	issues := map[string]db.Issue{}
	if len(candidates) > 0 {
		if len(decisionIDs) > 0 {
			drows, err := h.Queries.ListIssueDecisionsByIDs(r.Context(), db.ListIssueDecisionsByIDsParams{Ids: decisionIDs, IssueIds: issueIDs})
			if err != nil {
				slog.Warn("list inbox decisions: resolve decisions failed", append(logger.RequestAttrs(r), "error", err)...)
				writeError(w, http.StatusInternalServerError, "failed to list decisions")
				return
			}
			for _, d := range drows {
				decisions[uuidToString(d.ID)] = d
			}
		}
		if hasTransition {
			if reqs, err := h.Queries.ListPendingIssueTransitionRequestsForWorkspace(r.Context(), wsUUID); err == nil {
				for _, req := range reqs {
					transitions[uuidToString(req.ID)] = req
				}
			} else {
				slog.Warn("list inbox decisions: resolve transitions failed", append(logger.RequestAttrs(r), "error", err)...)
			}
		}
		if hasGoalQuestion {
			if grows, err := h.Queries.ListWaitingIssueGoalsForWorkspace(r.Context(), wsUUID); err == nil {
				for _, goal := range grows {
					goals[uuidToString(goal.ID)] = goal
				}
			} else {
				slog.Warn("list inbox decisions: resolve goal questions failed", append(logger.RequestAttrs(r), "error", err)...)
			}
		}
		irows, err := h.Queries.ListIssuesByIDsInWorkspace(r.Context(), db.ListIssuesByIDsInWorkspaceParams{WorkspaceID: wsUUID, IssueIds: issueIDs})
		if err != nil {
			slog.Warn("list inbox decisions: resolve issues failed", append(logger.RequestAttrs(r), "error", err)...)
		}
		for _, issue := range irows {
			issues[uuidToString(issue.ID)] = issue
		}
	}
	items := make([]InboxDecisionItem, 0)
	for _, c := range candidates {
		row := c.row
		base := db.ListInboxItemsRow(row)
		score, _ := attentionScore(base, now)
		item := InboxDecisionItem{InboxItemID: uuidToString(row.ID), IssueID: uuidToString(row.IssueID), IssueTitle: row.Title, RiskScore: score, Source: c.source}
		switch c.source {
		case ApprovalSourceDecision:
			decision, ok := decisions[c.entityID]
			// The card names one issue; a decision that moved to another issue is not it.
			if !ok || decision.IssueID != row.IssueID || len(decision.Response) > 0 {
				continue
			}
			resp := issueDecisionToResponse(decision)
			resp.Learned = h.decisionHint(r.Context(), wsUUID, userID, decision)
			item.Decision = &resp
			item.sortDeadline = resp.SlaDeadlineAt
			item.sortCreated = resp.CreatedAt
		case ApprovalSourceTransition:
			req, ok := transitions[c.entityID]
			if !ok || req.IssueID != row.IssueID {
				continue
			}
			item.Transition = &ApprovalTransition{RequestID: uuidToString(req.ID), FromStatus: req.FromStatus, ToStatus: req.ToStatus, RuleID: uuidToPtr(req.RuleID), ApproverRoles: issuestatus.ApproverRolesFor(h.transitionRule(r.Context(), req))}
			item.sortCreated = timestampToString(req.CreatedAt)
		case ApprovalSourceGoalQuestion:
			goal, ok := goals[c.entityID]
			if !ok || goal.IssueID != row.IssueID {
				continue
			}
			q := service.GoalQuestionOf(goal.Question)
			if q == nil || q.Answer != "" {
				continue
			}
			item.GoalQuestion = q
			item.sortCreated = q.AskedAt
		}
		if issue, ok := issues[uuidToString(row.IssueID)]; ok {
			item.IssueIdentifier = prefix + "-" + strconv.Itoa(int(issue.Number))
			item.IssueTitle = issue.Title
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.RiskScore != b.RiskScore {
			return a.RiskScore > b.RiskScore
		}
		da, db := a.sortDeadline, b.sortDeadline
		if (da == nil) != (db == nil) {
			return da != nil
		}
		if da != nil && *da != *db {
			return *da < *db
		}
		return a.sortCreated < b.sortCreated
	})
	total := len(items)
	if len(items) > inboxDecisionsCap {
		items = items[:inboxDecisionsCap]
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": items, "total": total})
}

func decisionUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}
