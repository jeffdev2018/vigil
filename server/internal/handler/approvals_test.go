package handler

// Inline approvals (OS plan, chantier 3): one feed for every ask a human
// must settle, the gate behind a card, who may decide, the events that let a
// timeline or an inbox row update in place, and the sweeper that settles a
// gate nobody answered.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func listApprovals(t *testing.T, query string) ApprovalsResponse {
	t.Helper()
	var out ApprovalsResponse
	testutil.Call(t, testHandler.ListApprovals, newRequest(http.MethodGet, "/api/approvals"+query, nil)).Want(http.StatusOK).JSON(&out)
	return out
}

func findApproval(items []ApprovalItem, source, id string) *ApprovalItem {
	for i := range items {
		if items[i].Source == source && items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func captureApprovalEvents(t *testing.T, eventType string) func() []map[string]any {
	t.Helper()
	got := make(chan map[string]any, 32)
	testHandler.Bus.Subscribe(eventType, func(e events.Event) {
		if payload, ok := e.Payload.(map[string]any); ok {
			select {
			case got <- payload:
			default:
			}
		}
	})
	return func() []map[string]any {
		var out []map[string]any
		deadline := time.After(2 * time.Second)
		for {
			select {
			case p := <-got:
				out = append(out, p)
			case <-deadline:
				return out
			default:
				if len(out) > 0 {
					return out
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
}

func openTestGate(t *testing.T, label string) (issueID, taskID, agentID string, gate ApprovalGateResponse) {
	t.Helper()
	issueID, taskID, agentID = runningAgentRun(t, label)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM approval_gate_event WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
	})
	gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+taskID+"/gates",
		map[string]any{"gate_type": "mcp_tool_call", "summary": "delete_customer", "details": map[string]any{"params": map[string]any{"id": "c-1"}, "paths": []string{"crm/customers"}}},
		gateHeaders(taskID, agentID), "taskId", taskID).Want(http.StatusCreated).JSON(&gate)
	return
}

func TestApprovalsFeedListsEveryKindOfAsk(t *testing.T) {
	rememberSettings(t)
	asked := captureApprovalEvents(t, protocol.EventApprovalAsked)
	gateIssue, _, _, gate := openTestGate(t, "feed gate")

	// A held status move on a second issue.
	moveIssue := dbfx.Issue(t, "feed move", testutil.Cols{"status": "in_progress"})
	requestID := dbfx.Insert(t, "issue_transition_request", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": moveIssue, "from_status": "in_progress", "to_status": "done",
		"requested_by_type": "member", "requested_by_id": testUserID,
	})
	// A goal-loop question on a third issue.
	goalIssue := dbfx.Issue(t, "feed goal")
	goalID := dbfx.Insert(t, "issue_goal", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": testWorkspaceID, "issue_id": goalIssue, "goal": "Ship it", "status": "waiting_user",
		"question": `{"kind":"choice","prompt":"Which region first?","options":["EU","US"],"asked_at":"2026-09-09T10:00:00Z"}`,
	})

	feed := listApprovals(t, "")
	g := findApproval(feed.Approvals, ApprovalSourceDecision, *gate.DecisionID)
	if g == nil {
		t.Fatalf("gate card missing from the feed: %+v", feed.Approvals)
	}
	if g.Kind != ApprovalKindGate || g.Gate == nil || g.Gate.Status != "pending" || g.ExpiresAt == nil || !g.CanDecide || g.Issue.ID != gateIssue || g.AskedBy.Type != "agent" || g.AskedBy.Name == "" {
		t.Fatalf("gate item = %+v (gate %+v)", *g, g.Gate)
	}
	if len(g.Options) != 2 || g.Options[0].ID != "approve" {
		t.Fatalf("gate options = %+v", g.Options)
	}
	tr := findApproval(feed.Approvals, ApprovalSourceTransition, requestID)
	if tr == nil {
		t.Fatalf("transition missing from the feed")
	}
	if tr.Kind != ApprovalKindTransition || tr.Transition == nil || tr.Transition.ToStatus != "done" || !tr.CanDecide || len(tr.Options) != 2 || tr.Issue.Identifier == "" {
		t.Fatalf("transition item = %+v", *tr)
	}
	gq := findApproval(feed.Approvals, ApprovalSourceGoalQuestion, goalID)
	if gq == nil {
		t.Fatalf("goal question missing from the feed")
	}
	if gq.Kind != ApprovalKindGoalAsk || gq.Question != "Which region first?" || len(gq.Options) != 2 || gq.Options[1].Label != "US" || gq.GoalQuestion == nil {
		t.Fatalf("goal item = %+v", *gq)
	}
	if feed.Total < 3 {
		t.Fatalf("total = %d", feed.Total)
	}

	// The issue filter keeps only that issue's asks.
	only := listApprovals(t, "?issue_id="+moveIssue)
	if len(only.Approvals) != 1 || only.Approvals[0].ID != requestID {
		t.Fatalf("issue filter = %+v", only.Approvals)
	}
	if got := listApprovals(t, "?issue_id="+gateIssue); len(got.Approvals) != 1 || got.Approvals[0].Kind != ApprovalKindGate {
		t.Fatalf("gate issue filter = %+v", got.Approvals)
	}

	// Opening the gate announced an ask.
	found := false
	for _, p := range asked() {
		if p["source"] == ApprovalSourceDecision && p["id"] == *gate.DecisionID && p["kind"] == ApprovalKindGate && p["issue_id"] == gateIssue {
			found = true
		}
	}
	if !found {
		t.Fatalf("no approval:asked event for the gate")
	}
}

// ListApprovals batches decisionKind's four per-decision lookups (approval
// gate, agent effects, watchdog verdict, pipeline run) into one query each
// across the whole feed instead of resolving them per decision; this puts a
// gate-backed decision and a plain decision (neither a gate, a plan, an
// interview, a preview, a watchdog verdict nor a pipeline run) side by side
// in one feed call, so a batching bug that hands the gate's kind to the
// plain decision (or vice versa) fails this test instead of shipping
// silently.
func TestApprovalsFeedKindsAreNotMixedAcrossDecisions(t *testing.T) {
	rememberSettings(t)
	gateIssue, _, _, gate := openTestGate(t, "kind batch gate")

	plainIssue := dbfx.Issue(t, "kind batch plain")
	var plain decisionEnvelope
	askDecision(t, plainIssue, decisionBody()).Want(http.StatusCreated).JSON(&plain)

	feed := listApprovals(t, "")
	g := findApproval(feed.Approvals, ApprovalSourceDecision, *gate.DecisionID)
	p := findApproval(feed.Approvals, ApprovalSourceDecision, plain.Decision.ID)
	if g == nil || p == nil {
		t.Fatalf("both decisions must be in the feed: gate=%v plain=%v", g, p)
	}
	if g.Kind != ApprovalKindGate || g.Gate == nil || g.Issue.ID != gateIssue {
		t.Fatalf("gate decision must keep kind=gate with its own gate and issue: %+v", *g)
	}
	if p.Kind != ApprovalKindDecision || p.Gate != nil || p.Issue.ID != plainIssue {
		t.Fatalf("plain decision must keep kind=decision with no gate and its own issue: %+v", *p)
	}
}

func TestApprovalGatePolicyReservesGatesToOwnersAndAdmins(t *testing.T) {
	rememberSettings(t)
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"approval_gates":{"approvers":"owner_admin"}}'::jsonb WHERE id = $1`, testWorkspaceID)
	issue, _, _, gate := openTestGate(t, "policy gate")
	decided := captureApprovalEvents(t, protocol.EventApprovalDecided)

	dbfx.Exec(t, `UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	dbfx.Cleanup(t, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	item := findApproval(listApprovals(t, "?issue_id="+issue).Approvals, ApprovalSourceDecision, *gate.DecisionID)
	if item == nil || item.CanDecide || item.CannotDecideReason != "gate_approvers_policy" {
		t.Fatalf("member's view = %+v", item)
	}
	respondDecision(t, issue, *gate.DecisionID, map[string]any{"option_id": "approve"}).Want(http.StatusForbidden)

	dbfx.Exec(t, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	item = findApproval(listApprovals(t, "?issue_id="+issue).Approvals, ApprovalSourceDecision, *gate.DecisionID)
	if item == nil || !item.CanDecide {
		t.Fatalf("owner's view = %+v", item)
	}
	respondDecision(t, issue, *gate.DecisionID, map[string]any{"option_id": "approve"}).Want(http.StatusOK)
	if got := listApprovals(t, "?issue_id="+issue); len(got.Approvals) != 0 {
		t.Fatalf("settled gate still listed: %+v", got.Approvals)
	}
	found := false
	for _, p := range decided() {
		if p["id"] == *gate.DecisionID && p["outcome"] == "approve" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no approval:decided event with the approve outcome")
	}
}

func TestExpireOverdueGatesAnswersTheCardAndAnnouncesIt(t *testing.T) {
	rememberSettings(t)
	issue, task, agent, gate := openTestGate(t, "sweep gate")
	decided := captureApprovalEvents(t, protocol.EventApprovalDecided)
	dbfx.Exec(t, `UPDATE approval_gate_event SET expires_at = now() - interval '1 minute' WHERE id = $1`, gate.ID)

	if n := testHandler.ExpireOverdueGates(context.Background()); n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}
	var polled ApprovalGateResponse
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+gate.ID, nil, gateHeaders(task, agent), "taskId", task, "gateId", gate.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "expired" {
		t.Fatalf("gate after sweep = %+v", polled)
	}
	var responder, option string
	dbfx.QueryRow(t, `SELECT COALESCE(responded_by_type, ''), COALESCE(response->>'option_id', '') FROM issue_decision WHERE id = $1`, *gate.DecisionID).Scan(&responder, &option)
	if responder != "system" || option != "deny" {
		t.Fatalf("card after sweep: responded_by=%q option=%q", responder, option)
	}
	if got := listApprovals(t, "?issue_id="+issue); len(got.Approvals) != 0 {
		t.Fatalf("expired gate still listed: %+v", got.Approvals)
	}
	if n := testHandler.ExpireOverdueGates(context.Background()); n != 0 {
		t.Fatalf("second sweep expired %d gates", n)
	}
	found := false
	for _, p := range decided() {
		if p["id"] == *gate.DecisionID && p["outcome"] == "expired" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no approval:decided event with the expired outcome")
	}
}
