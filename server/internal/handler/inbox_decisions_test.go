package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Inbox zero (K63): my pending cards with their options, urgent first, at
// most five with the total; an answered card leaves the list; the list is
// workspace-scoped through the attention inbox query it projects.
//
// JEF-244: ?include=transitions,goal_questions widens the projection to held
// status moves and goal-loop questions; without the param the response is the
// Decision Cards it always was, and unknown include values are ignored.

type inboxDecisionsPayload struct {
	Decisions []InboxDecisionItem `json:"decisions"`
	Total     int                 `json:"total"`
}

func listInboxDecisionsIn(t *testing.T, workspaceID, query string) inboxDecisionsPayload {
	t.Helper()
	var out inboxDecisionsPayload
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxDecisions), inboxRequest(http.MethodGet, "/api/inbox/decisions"+query, workspaceID)).Want(http.StatusOK).JSON(&out)
	return out
}

func listInboxDecisions(t *testing.T, query string) inboxDecisionsPayload {
	t.Helper()
	return listInboxDecisionsIn(t, testWorkspaceID, query)
}

// inboxDecisionRow files one inbox row for the test user, the way the
// decision / transition-gate / goal-loop services do when they ask.
func inboxDecisionRow(t *testing.T, workspaceID, issueID, rowType string, details map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("details: %v", err)
	}
	return dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": workspaceID, "recipient_type": "member", "recipient_id": testUserID,
		"type": rowType, "severity": "action_required", "issue_id": issueID, "title": "inbox zero fixture",
		"details": string(raw),
	})
}

func findInboxDecision(items []InboxDecisionItem, source, issueID string) *InboxDecisionItem {
	for i := range items {
		if items[i].Source == source && items[i].IssueID == issueID {
			return &items[i]
		}
	}
	return nil
}

func TestInboxDecisionsCapAndOrder(t *testing.T) {
	type payload struct {
		Decisions []InboxDecisionItem `json:"decisions"`
		Total     int                 `json:"total"`
	}
	list := func() payload {
		t.Helper()
		var out payload
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxDecisions), inboxRequest(http.MethodGet, "/api/inbox/decisions", testWorkspaceID)).Want(http.StatusOK).JSON(&out)
		return out
	}
	before := list().Total
	var urgentID string
	for i := 0; i < 6; i++ {
		issue := dbfx.Issue(t, "decision inbox issue")
		body := decisionBody()
		if i == 3 {
			body["urgency"] = "high"
		}
		var created decisionEnvelope
		askDecision(t, issue, body).Want(http.StatusCreated).JSON(&created)
		if i == 3 {
			urgentID = created.Decision.ID
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
			testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		})
	}
	got := list()
	if got.Total != before+6 || len(got.Decisions) != inboxDecisionsCap {
		t.Fatalf("total %d (before %d), listed %d", got.Total, before, len(got.Decisions))
	}
	// Urgent cards come first (other tests may leave urgent cards behind; ours must be among them).
	var urgent *InboxDecisionItem
	for i := range got.Decisions {
		if got.Decisions[i].Decision.ID == urgentID {
			urgent = &got.Decisions[i]
		}
	}
	if got.Decisions[0].Decision.Urgency != "high" || urgent == nil || urgent.RiskScore < got.Decisions[len(got.Decisions)-1].RiskScore {
		t.Fatalf("ordering = %+v", got.Decisions)
	}
	if len(urgent.Decision.Options) == 0 || urgent.IssueIdentifier == "" || urgent.IssueTitle != "decision inbox issue" {
		t.Fatalf("urgent card = %+v", urgent)
	}
	// Answering from the list removes the card and lowers the total.
	respondDecision(t, urgent.IssueID, urgent.Decision.ID, map[string]any{"option_id": "keep"}).Want(http.StatusOK)
	got = list()
	if got.Total != before+5 {
		t.Fatalf("total after answer = %d, want %d", got.Total, before+5)
	}
	for _, d := range got.Decisions {
		if d.Decision.ID == urgentID {
			t.Fatal("an answered card must leave the list")
		}
	}
}

// The no-param response is the pre-JEF-244 projection: Decision Cards only,
// with the pre-JEF-244 keys plus the always-present source.
func TestInboxDecisionsDefaultShapeLocked(t *testing.T) {
	issue := dbfx.Issue(t, "shape lock card")
	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
	})
	// Rows of the two other sources exist but stay invisible without include.
	moveIssue := dbfx.Issue(t, "shape lock move", testutil.Cols{"status": "in_progress"})
	requestID := dbfx.Insert(t, "issue_transition_request", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": moveIssue, "from_status": "in_progress", "to_status": "done",
		"requested_by_type": "member", "requested_by_id": testUserID,
	})
	inboxDecisionRow(t, testWorkspaceID, moveIssue, "transition_approval_requested", map[string]any{"request_id": requestID, "from": "in_progress", "to": "done"})
	goalIssue := dbfx.Issue(t, "shape lock ask")
	goalID := dbfx.Insert(t, "issue_goal", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": testWorkspaceID, "issue_id": goalIssue, "goal": "Ship it", "status": "waiting_user",
		"question": `{"kind":"choice","prompt":"Which region first?","options":["EU","US"],"asked_at":"2026-09-09T10:00:00Z"}`,
	})
	inboxDecisionRow(t, testWorkspaceID, goalIssue, "goal_question", map[string]any{"goal_id": goalID})

	var raw struct {
		Decisions []map[string]any `json:"decisions"`
		Total     int              `json:"total"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxDecisions), inboxRequest(http.MethodGet, "/api/inbox/decisions", testWorkspaceID)).Want(http.StatusOK).JSON(&raw)
	allowed := map[string]bool{"inbox_item_id": true, "issue_id": true, "issue_identifier": true, "issue_title": true, "risk_score": true, "source": true, "decision": true}
	found := false
	for _, item := range raw.Decisions {
		for k := range item {
			if !allowed[k] {
				t.Fatalf("unexpected key %q in no-param entry: %v", k, item)
			}
		}
		if item["source"] != "decision" {
			t.Fatalf("no-param entry source = %v", item["source"])
		}
		if item["issue_id"] == moveIssue || item["issue_id"] == goalIssue {
			t.Fatalf("no-param list included %v", item["issue_id"])
		}
		if item["decision"] == nil {
			t.Fatalf("decision payload missing: %v", item)
		}
		if item["issue_id"] == issue {
			found = true
		}
	}
	if !found {
		t.Fatal("the decision card is missing from the no-param list")
	}
}

func TestInboxDecisionsIncludeTransitions(t *testing.T) {
	issue := dbfx.Issue(t, "held move", testutil.Cols{"status": "in_progress"})
	requestID := dbfx.Insert(t, "issue_transition_request", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issue, "from_status": "in_progress", "to_status": "done",
		"requested_by_type": "member", "requested_by_id": testUserID,
	})
	// Two inbox rows for the same request: the projection dedups per entity.
	inboxDecisionRow(t, testWorkspaceID, issue, "transition_approval_requested", map[string]any{"request_id": requestID, "from": "in_progress", "to": "done"})
	inboxDecisionRow(t, testWorkspaceID, issue, "transition_approval_requested", map[string]any{"request_id": requestID, "from": "in_progress", "to": "done"})

	// Approved underneath its inbox row: not an ask any more.
	settledIssue := dbfx.Issue(t, "settled move")
	settledID := dbfx.Insert(t, "issue_transition_request", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": settledIssue, "from_status": "todo", "to_status": "done", "state": "approved",
		"requested_by_type": "member", "requested_by_id": testUserID,
	})
	inboxDecisionRow(t, testWorkspaceID, settledIssue, "transition_approval_requested", map[string]any{"request_id": settledID})

	got := listInboxDecisions(t, "?include=transitions")
	tr := findInboxDecision(got.Decisions, "transition", issue)
	if tr == nil {
		t.Fatalf("held move missing: %+v", got.Decisions)
	}
	n := 0
	for _, d := range got.Decisions {
		if d.Transition != nil && d.Transition.RequestID == requestID {
			n++
		}
		if d.Transition != nil && d.Transition.RequestID == settledID {
			t.Fatalf("settled request listed: %+v", d)
		}
	}
	if n != 1 {
		t.Fatalf("dedup: %d entries for one request", n)
	}
	if tr.Transition == nil || tr.Transition.FromStatus != "in_progress" || tr.Transition.ToStatus != "done" || tr.Transition.RuleID != nil {
		t.Fatalf("transition payload = %+v", tr.Transition)
	}
	if len(tr.Transition.ApproverRoles) != 2 || tr.Transition.ApproverRoles[0] != "admin" || tr.Transition.ApproverRoles[1] != "owner" {
		t.Fatalf("approver roles = %v", tr.Transition.ApproverRoles)
	}
	if tr.Decision != nil || tr.GoalQuestion != nil {
		t.Fatalf("transition entry carries another payload: %+v", tr)
	}
	if tr.IssueIdentifier == "" || tr.IssueTitle != "held move" {
		t.Fatalf("issue projection = %+v", tr)
	}
}

func TestInboxDecisionsIncludeGoalQuestions(t *testing.T) {
	issue := dbfx.Issue(t, "goal ask")
	goalID := dbfx.Insert(t, "issue_goal", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": testWorkspaceID, "issue_id": issue, "goal": "Ship it", "status": "waiting_user",
		"question": `{"kind":"choice","prompt":"Which region first?","options":["EU","US"],"asked_at":"2026-09-09T10:00:00Z"}`,
	})
	inboxDecisionRow(t, testWorkspaceID, issue, "goal_question", map[string]any{"goal_id": goalID})

	// Answered underneath its inbox row: settled.
	answeredIssue := dbfx.Issue(t, "answered ask")
	answeredGoalID := dbfx.Insert(t, "issue_goal", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": testWorkspaceID, "issue_id": answeredIssue, "goal": "Ship it", "status": "waiting_user",
		"question": `{"kind":"text","prompt":"Old question?","asked_at":"2026-09-08T10:00:00Z","answer":"Postgres"}`,
	})
	inboxDecisionRow(t, testWorkspaceID, answeredIssue, "goal_question", map[string]any{"goal_id": answeredGoalID})

	got := listInboxDecisions(t, "?include=goal_questions")
	gq := findInboxDecision(got.Decisions, "goal_question", issue)
	if gq == nil {
		t.Fatalf("goal question missing: %+v", got.Decisions)
	}
	if gq.GoalQuestion == nil || gq.GoalQuestion.Prompt != "Which region first?" || gq.GoalQuestion.Kind != "choice" || len(gq.GoalQuestion.Options) != 2 || gq.GoalQuestion.Options[1] != "US" {
		t.Fatalf("goal question payload = %+v", gq.GoalQuestion)
	}
	if gq.Decision != nil || gq.Transition != nil {
		t.Fatalf("goal entry carries another payload: %+v", gq)
	}
	if gq.IssueIdentifier == "" || gq.IssueTitle != "goal ask" {
		t.Fatalf("issue projection = %+v", gq)
	}
	if d := findInboxDecision(got.Decisions, "goal_question", answeredIssue); d != nil {
		t.Fatalf("answered question listed: %+v", d)
	}
}

// Both sources together: one merged list, risk then SLA ordered, capped at
// five with the total counting everything included; unknown include values
// are ignored.
func TestInboxDecisionsIncludeMergesSortsAndCaps(t *testing.T) {
	ws := dbfx.Workspace(t, "Inbox decision merge", "inbox-merge-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")

	mkDecision := func(title, urgency string, sla bool) string {
		issue := dbfx.Issue(t, title, testutil.Cols{"workspace_id": ws})
		cols := testutil.Cols{
			"workspace_id": ws, "issue_id": issue, "asked_by_type": "member", "asked_by_id": testUserID,
			"question": "Proceed?", "options": `[{"id":"a","label":"A"},{"id":"b","label":"B"}]`, "urgency": urgency,
		}
		if sla {
			cols["sla_deadline_at"] = testutil.Raw("now() + interval '1 day'")
		}
		decisionID := dbfx.Insert(t, "issue_decision", cols)
		inboxDecisionRow(t, ws, issue, "decision_request", map[string]any{"decision_id": decisionID, "urgency": urgency})
		return decisionID
	}
	slaID := mkDecision("sla card", "normal", true)
	plainID := mkDecision("plain card", "normal", false)
	urgentID := mkDecision("urgent card", "high", false)
	for _, to := range []string{"done", "cancelled"} {
		issue := dbfx.Issue(t, "merge move "+to, testutil.Cols{"workspace_id": ws, "status": "in_progress"})
		requestID := dbfx.Insert(t, "issue_transition_request", testutil.Cols{
			"workspace_id": ws, "issue_id": issue, "from_status": "in_progress", "to_status": to,
			"requested_by_type": "member", "requested_by_id": testUserID,
		})
		inboxDecisionRow(t, ws, issue, "transition_approval_requested", map[string]any{"request_id": requestID})
	}
	goalIssue := dbfx.Issue(t, "merge ask", testutil.Cols{"workspace_id": ws})
	goalID := dbfx.Insert(t, "issue_goal", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": ws, "issue_id": goalIssue, "goal": "Ship it", "status": "waiting_user",
		"question": `{"kind":"text","prompt":"Which name?","asked_at":"2026-09-09T10:00:00Z"}`,
	})
	inboxDecisionRow(t, ws, goalIssue, "goal_question", map[string]any{"goal_id": goalID})

	got := listInboxDecisionsIn(t, ws, "?include=transitions,goal_questions")
	if got.Total != 6 || len(got.Decisions) != inboxDecisionsCap {
		t.Fatalf("total %d, listed %d; want 6 total, %d listed", got.Total, len(got.Decisions), inboxDecisionsCap)
	}
	pos := func(decisionID string) int {
		for i, d := range got.Decisions {
			if d.Decision != nil && d.Decision.ID == decisionID {
				return i
			}
		}
		return -1
	}
	// The urgent card outranks everything; between equal-risk cards the one
	// with an SLA deadline comes first.
	if got.Decisions[0].Decision == nil || got.Decisions[0].Decision.ID != urgentID {
		t.Fatalf("first = %+v", got.Decisions[0])
	}
	if ps, pp := pos(slaID), pos(plainID); ps == -1 || pp == -1 || ps > pp {
		t.Fatalf("sla at %d, plain at %d", ps, pp)
	}
	sources := map[string]bool{}
	for _, d := range got.Decisions {
		sources[d.Source] = true
	}
	if !sources["transition"] || !sources["goal_question"] {
		t.Fatalf("sources listed = %v", sources)
	}

	// Unknown include values are ignored: the list falls back to cards only.
	unk := listInboxDecisionsIn(t, ws, "?include=bogus")
	if unk.Total != 3 {
		t.Fatalf("include=bogus total = %d, want 3", unk.Total)
	}
	for _, d := range unk.Decisions {
		if d.Source != "decision" {
			t.Fatalf("include=bogus listed %q", d.Source)
		}
	}
}
