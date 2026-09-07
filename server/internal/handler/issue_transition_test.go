package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Transition rules and approval gates (F28), HTTP surface.
//
// The origin x target x role x actor-type x project-override matrix is the
// resolver's, and lives in internal/issuestatus/transition_test.go. This file
// covers what only a handler can: which entry points run the gate, the wire
// shape of a refusal and of a held write, that the issue is untouched in both
// cases, and the decision concurrency fence.

// transitionRule inserts a rule and returns its id.
func transitionRule(t *testing.T, cols testutil.Cols) string {
	t.Helper()
	base := testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"to_category":       "done",
		"allowed_roles":     testutil.Raw("ARRAY[]::text[]"),
		"allow_actor_types": testutil.Raw("ARRAY[]::text[]"),
		"approver_roles":    testutil.Raw("ARRAY[]::text[]"),
	}
	for k, v := range cols {
		base[k] = v
	}
	return dbfx.Insert(t, "issue_transition_rule", base)
}

// memberRequest builds a request authenticated as a second user holding role.
// The suite's default user is the workspace OWNER, who is exempt from every
// rule — driving these tests as them would pass whatever the gate did.
var f28UserSeq atomic.Int64

func memberRequest(t *testing.T, role, method, path string, body any) *http.Request {
	t.Helper()
	email := fmt.Sprintf("f28-%s-%d@example.test", role, f28UserSeq.Add(1))
	userID := dbfx.User(t, "F28 "+role, email)
	dbfx.Member(t, testWorkspaceID, userID, role)
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	return req
}

func issueStatus(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status)
	return status
}

// Acceptance 1: a workspace with no rules behaves exactly as before.
func TestIssueTransitionNoRulesLeavesBehaviourUnchanged(t *testing.T) {
	issueID := dbfx.Issue(t, "F28 free move", testutil.Cols{"status": "in_progress"})

	req := memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(req, "id", issueID)).Want(http.StatusOK)

	if got := issueStatus(t, issueID); got != "done" {
		t.Fatalf("status = %q, want done", got)
	}
}

// Acceptance 2 + 3: one rule, two roles, two answers. The refusal must leave
// the row untouched — a gate that answers 403 after writing is worse than none.
func TestIssueTransitionDeniesAndAllowsByRole(t *testing.T) {
	transitionRule(t, testutil.Cols{
		"from_category": "in_progress",
		"to_category":   "done",
		"allowed_roles": testutil.Raw("ARRAY['admin']::text[]"),
	})

	t.Run("member is refused with a coded 403", func(t *testing.T) {
		issueID := dbfx.Issue(t, "F28 refused", testutil.Cols{"status": "in_progress"})
		req := memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})

		var body map[string]any
		testutil.Call(t, testHandler.UpdateIssue, withURLParam(req, "id", issueID)).
			Want(http.StatusForbidden).JSON(&body)

		if body["code"] != ErrCodeTransitionNotAllowed {
			t.Fatalf("code = %v, want %q", body["code"], ErrCodeTransitionNotAllowed)
		}
		if body["from"] != "in_progress" || body["to"] != "done" {
			t.Fatalf("from/to = %v/%v, want in_progress/done", body["from"], body["to"])
		}
		if body["rule_id"] == nil || body["rule_id"] == "" {
			t.Fatalf("rule_id missing from %v — the client cannot name the rule that refused", body)
		}
		if got := issueStatus(t, issueID); got != "in_progress" {
			t.Fatalf("status = %q, want in_progress — a refusal must not write", got)
		}
	})

	t.Run("admin passes the same rule", func(t *testing.T) {
		issueID := dbfx.Issue(t, "F28 allowed", testutil.Cols{"status": "in_progress"})
		req := memberRequest(t, "admin", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
		testutil.Call(t, testHandler.UpdateIssue, withURLParam(req, "id", issueID)).Want(http.StatusOK)

		if got := issueStatus(t, issueID); got != "done" {
			t.Fatalf("status = %q, want done", got)
		}
	})
}

// Acceptance 4: a nominative grant reaches exactly the agent it names.
func TestIssueTransitionNominativeAgentGrant(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "F28 runtime")
	// autonomous so the trust dial (K26), which runs before this gate, does not
	// answer first — this test is about the transition rule, not the dial.
	granted := dbfx.Agent(t, "F28 granted", runtimeID, testutil.Cols{"trust_mode": "autonomous"})
	other := dbfx.Agent(t, "F28 other", runtimeID, testutil.Cols{"trust_mode": "autonomous"})
	ruleID := transitionRule(t, testutil.Cols{
		"from_category": "in_progress",
		"to_category":   "done",
	})
	dbfx.Insert(t, "issue_transition_rule_actor", testutil.Cols{
		"rule_id": ruleID, "actor_type": "agent", "actor_id": granted,
	})

	call := func(t *testing.T, agentID string) int {
		issueID := dbfx.Issue(t, "F28 agent move", testutil.Cols{"status": "in_progress"})
		taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
		req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
		return testutil.Call(t, testHandler.UpdateIssue, withURLParam(req, "id", issueID)).Code
	}

	if code := call(t, granted); code != http.StatusOK {
		t.Fatalf("granted agent got %d, want 200", code)
	}
	if code := call(t, other); code != http.StatusForbidden {
		t.Fatalf("ungranted agent got %d, want 403", code)
	}
}

// Acceptance 6: requires_approval holds the write. 202, the issue as it still
// is, and a pending request. Filing a second one is 409.
func TestIssueTransitionHoldsForApproval(t *testing.T) {
	transitionRule(t, testutil.Cols{
		"from_category":     "in_progress",
		"to_category":       "done",
		"allowed_roles":     testutil.Raw("ARRAY['member']::text[]"),
		"requires_approval": true,
		"approver_roles":    testutil.Raw("ARRAY['admin']::text[]"),
	})
	issueID := dbfx.Issue(t, "F28 held", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_transition_request WHERE issue_id = $1`, issueID)
	})

	req := memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	var body map[string]any
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(req, "id", issueID)).
		Want(http.StatusAccepted).JSON(&body)

	if body["status"] != "pending_approval" {
		t.Fatalf("status = %v, want pending_approval", body["status"])
	}
	requestID, _ := body["request_id"].(string)
	if requestID == "" {
		t.Fatalf("request_id missing from %v", body)
	}
	issue, _ := body["issue"].(map[string]any)
	if issue == nil || issue["status"] != "in_progress" {
		t.Fatalf("echoed issue = %v, want the unchanged in_progress issue", issue)
	}
	if got := issueStatus(t, issueID); got != "in_progress" {
		t.Fatalf("status = %q, want in_progress — a held write must not land", got)
	}

	// A second attempt reports the request already waiting rather than filing
	// a duplicate; the unique partial index is what makes that safe.
	second := memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	var conflict map[string]any
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(second, "id", issueID)).
		Want(http.StatusConflict).JSON(&conflict)
	if conflict["code"] != ErrCodeTransitionPending {
		t.Fatalf("code = %v, want %q", conflict["code"], ErrCodeTransitionPending)
	}
	if conflict["request_id"] != requestID {
		t.Fatalf("request_id = %v, want the pending one %q", conflict["request_id"], requestID)
	}
}

// Acceptance 7: approving applies the move and says so on the wire.
func TestIssueTransitionApproveAppliesAndPublishes(t *testing.T) {
	transitionRule(t, testutil.Cols{
		"from_category":     "in_progress",
		"to_category":       "done",
		"allowed_roles":     testutil.Raw("ARRAY['member']::text[]"),
		"requires_approval": true,
	})
	issueID := dbfx.Issue(t, "F28 approve", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_transition_request WHERE issue_id = $1`, issueID)
	})

	var held map[string]any
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(
		memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"}), "id", issueID)).
		Want(http.StatusAccepted).JSON(&held)
	requestID, _ := held["request_id"].(string)

	var mu sync.Mutex
	var statusChanged []bool
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, _ := payload["issue"].(IssueResponse)
		if issue.ID != issueID {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		changed, _ := payload["status_changed"].(bool)
		statusChanged = append(statusChanged, changed)
	})

	approve := newRequest("POST", "/api/issue-transition-requests/"+requestID+"/approve", map[string]any{"note": "ok"})
	var out map[string]any
	testutil.Call(t, testHandler.ApproveIssueTransitionRequest, withURLParam(approve, "id", requestID)).
		Want(http.StatusOK).JSON(&out)

	if got := issueStatus(t, issueID); got != "done" {
		t.Fatalf("status = %q, want done after approval", got)
	}
	request, _ := out["request"].(map[string]any)
	if request == nil || request["state"] != "approved" {
		t.Fatalf("request = %v, want state approved", request)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(statusChanged) == 0 {
		t.Fatalf("no issue:updated published for the approved move")
	}
	for _, changed := range statusChanged {
		if changed {
			return
		}
	}
	t.Fatalf("issue:updated published without status_changed=true: %v", statusChanged)
}

// Acceptance 8: rejecting closes the request and honours reject_status_key.
func TestIssueTransitionRejectMovesToFallbackStatus(t *testing.T) {
	transitionRule(t, testutil.Cols{
		"from_category":     "in_progress",
		"to_category":       "done",
		"allowed_roles":     testutil.Raw("ARRAY['member']::text[]"),
		"requires_approval": true,
		"reject_status_key": "blocked",
	})
	issueID := dbfx.Issue(t, "F28 reject", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_transition_request WHERE issue_id = $1`, issueID)
	})

	var held map[string]any
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(
		memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"}), "id", issueID)).
		Want(http.StatusAccepted).JSON(&held)
	requestID, _ := held["request_id"].(string)

	reject := newRequest("POST", "/api/issue-transition-requests/"+requestID+"/reject", map[string]any{"note": "not yet"})
	testutil.Call(t, testHandler.RejectIssueTransitionRequest, withURLParam(reject, "id", requestID)).Want(http.StatusOK)

	if got := issueStatus(t, issueID); got != "blocked" {
		t.Fatalf("status = %q, want blocked — the rule names it as the reject fallback", got)
	}
	var state string
	dbfx.QueryRow(t, `SELECT state FROM issue_transition_request WHERE id = $1`, requestID).Scan(&state)
	if state != "rejected" {
		t.Fatalf("state = %q, want rejected", state)
	}
}

// Acceptance 12: two approvers race, one wins, the loser is told why.
func TestIssueTransitionSecondDeciderGets409(t *testing.T) {
	transitionRule(t, testutil.Cols{
		"from_category":     "in_progress",
		"to_category":       "done",
		"allowed_roles":     testutil.Raw("ARRAY['member']::text[]"),
		"requires_approval": true,
	})
	issueID := dbfx.Issue(t, "F28 race", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_transition_request WHERE issue_id = $1`, issueID)
	})

	var held map[string]any
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(
		memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"}), "id", issueID)).
		Want(http.StatusAccepted).JSON(&held)
	requestID, _ := held["request_id"].(string)

	first := newRequest("POST", "/x/approve", nil)
	testutil.Call(t, testHandler.ApproveIssueTransitionRequest, withURLParam(first, "id", requestID)).Want(http.StatusOK)

	second := newRequest("POST", "/x/reject", nil)
	var body map[string]any
	testutil.Call(t, testHandler.RejectIssueTransitionRequest, withURLParam(second, "id", requestID)).
		Want(http.StatusConflict).JSON(&body)
	if body["code"] != ErrCodeAlreadyDecided {
		t.Fatalf("code = %v, want %q", body["code"], ErrCodeAlreadyDecided)
	}
}

// Acceptance 9: a project rule overrides the workspace rule for its project
// only. The resolver owns the precedence; this proves the handler feeds it the
// issue's project.
func TestIssueTransitionProjectRuleOverridesWorkspace(t *testing.T) {
	projectID := dbfx.Project(t, "F28 governed")
	transitionRule(t, testutil.Cols{
		"to_category":       "done",
		"allow_actor_types": testutil.Raw("ARRAY['member']::text[]"),
	})
	transitionRule(t, testutil.Cols{
		"project_id":  projectID,
		"to_category": "done",
	})

	governed := dbfx.Issue(t, "F28 in project", testutil.Cols{"status": "in_progress", "project_id": projectID})
	free := dbfx.Issue(t, "F28 no project", testutil.Cols{"status": "in_progress"})

	req := memberRequest(t, "member", "PUT", "/api/issues/"+governed, map[string]any{"status": "done"})
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(req, "id", governed)).Want(http.StatusForbidden)

	req2 := memberRequest(t, "member", "PUT", "/api/issues/"+free, map[string]any{"status": "done"})
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(req2, "id", free)).Want(http.StatusOK)
}

// Acceptance 10: a batch refuses PER ISSUE. The other items still move, and
// the response says which did not and why.
func TestIssueBatchTransitionPartialRefusal(t *testing.T) {
	projectID := dbfx.Project(t, "F28 batch governed")
	transitionRule(t, testutil.Cols{
		"project_id":  projectID,
		"to_category": "done",
	})
	governed := dbfx.Issue(t, "F28 batch refused", testutil.Cols{"status": "in_progress", "project_id": projectID})
	free := dbfx.Issue(t, "F28 batch allowed", testutil.Cols{"status": "in_progress"})

	req := memberRequest(t, "member", "PUT", "/api/issues/batch", map[string]any{
		"issue_ids": []string{governed, free},
		"updates":   map[string]any{"status": "done"},
	})
	var body struct {
		Updated int              `json:"updated"`
		Refused []map[string]any `json:"refused"`
	}
	testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK).JSON(&body)

	if body.Updated != 1 {
		t.Fatalf("updated = %d, want 1 — the ungoverned issue must still move", body.Updated)
	}
	if len(body.Refused) != 1 || body.Refused[0]["issue_id"] != governed {
		t.Fatalf("refused = %v, want exactly the governed issue %s", body.Refused, governed)
	}
	if body.Refused[0]["code"] != ErrCodeTransitionNotAllowed {
		t.Fatalf("refused code = %v, want %q", body.Refused[0]["code"], ErrCodeTransitionNotAllowed)
	}
	if got := issueStatus(t, governed); got != "in_progress" {
		t.Fatalf("governed status = %q, want in_progress", got)
	}
	if got := issueStatus(t, free); got != "done" {
		t.Fatalf("free status = %q, want done", got)
	}
}

// A create landing outside the default category goes through the rules; an
// ordinary create never does, or a rule on `todo` would stop people filing
// tickets at all.
func TestCreateIssueTransitionGate(t *testing.T) {
	transitionRule(t, testutil.Cols{"to_category": "done"})
	transitionRule(t, testutil.Cols{"to_category": "todo"})

	refused := memberRequest(t, "member", "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "F28 create done", "status": "done",
	})
	var body map[string]any
	testutil.Call(t, testHandler.CreateIssue, refused).Want(http.StatusForbidden).JSON(&body)
	if body["code"] != ErrCodeTransitionNotAllowed {
		t.Fatalf("code = %v, want %q", body["code"], ErrCodeTransitionNotAllowed)
	}

	allowed := memberRequest(t, "member", "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "F28 create todo", "status": "todo",
	})
	var created map[string]any
	testutil.Call(t, testHandler.CreateIssue, allowed).Want(http.StatusCreated).JSON(&created)
	if id, _ := created["id"].(string); id != "" {
		dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, id)
	}
}

// The effective endpoint is what the picker greys options from.
func TestEffectiveIssueTransitions(t *testing.T) {
	transitionRule(t, testutil.Cols{
		"to_category":       "done",
		"allowed_roles":     testutil.Raw("ARRAY['admin']::text[]"),
		"requires_approval": true,
	})
	issueID := dbfx.Issue(t, "F28 effective", testutil.Cols{"status": "in_progress"})

	read := func(t *testing.T, role string) map[string]map[string]any {
		req := memberRequest(t, role, "GET", "/api/issue-transition-rules/effective?issue_id="+issueID, nil)
		var body struct {
			FromCategory string           `json:"from_category"`
			Transitions  []map[string]any `json:"transitions"`
		}
		testutil.Call(t, testHandler.EffectiveIssueTransitions, req).Want(http.StatusOK).JSON(&body)
		if body.FromCategory != "in_progress" {
			t.Fatalf("from_category = %q, want in_progress", body.FromCategory)
		}
		out := map[string]map[string]any{}
		for _, entry := range body.Transitions {
			key, _ := entry["to_category"].(string)
			out[key] = entry
		}
		return out
	}

	forMember := read(t, "member")
	if forMember["done"]["allowed"] != false {
		t.Fatalf("done for a member = %v, want allowed:false", forMember["done"])
	}
	if forMember["cancelled"]["allowed"] != true {
		t.Fatalf("cancelled = %v, want allowed:true — no rule governs it", forMember["cancelled"])
	}

	forAdmin := read(t, "admin")
	if forAdmin["done"]["allowed"] != true || forAdmin["done"]["requires_approval"] != true {
		t.Fatalf("done for an admin = %v, want allowed with requires_approval", forAdmin["done"])
	}
}

// Writes are owner/admin; reads are any member.
func TestIssueTransitionRuleWritesAreAdminOnly(t *testing.T) {
	body := map[string]any{"to_category": "done", "allowed_roles": []string{"admin"}}

	create := memberRequest(t, "member", "POST", "/api/issue-transition-rules?workspace_id="+testWorkspaceID, body)
	testutil.Call(t, testHandler.CreateIssueTransitionRule, create).Want(http.StatusForbidden)

	admin := memberRequest(t, "admin", "POST", "/api/issue-transition-rules?workspace_id="+testWorkspaceID, body)
	var created IssueTransitionRuleResponse
	testutil.Call(t, testHandler.CreateIssueTransitionRule, admin).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM issue_transition_rule WHERE id = $1`, created.ID)
	if created.ToCategory != "done" || len(created.AllowedRoles) != 1 {
		t.Fatalf("created = %+v, want to_category done with one allowed role", created)
	}

	read := memberRequest(t, "member", "GET", "/api/issue-transition-rules?workspace_id="+testWorkspaceID, nil)
	testutil.Call(t, testHandler.ListIssueTransitionRules, read).Want(http.StatusOK)
}

// A malformed body is a 400, not a panic or a half-written rule.
func TestIssueTransitionRuleRejectsMalformedBody(t *testing.T) {
	cases := []struct {
		name string
		body any
	}{
		{"not json", "{"},
		{"unknown category", map[string]any{"to_category": "shipped"}},
		{"unknown role", map[string]any{"to_category": "done", "allowed_roles": []string{"root"}}},
		{"unknown actor type", map[string]any{"to_category": "done", "allow_actor_types": []string{"robot"}}},
		{"non-uuid actor", map[string]any{"to_category": "done", "actors": []map[string]any{{"actor_type": "member", "actor_id": "nope"}}}},
		{"unknown reject status", map[string]any{"to_category": "done", "reject_status_key": "nowhere"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := memberRequest(t, "admin", "POST", "/api/issue-transition-rules?workspace_id="+testWorkspaceID, tc.body)
			testutil.Call(t, testHandler.CreateIssueTransitionRule, req).Want(http.StatusBadRequest)
		})
	}
}

// The requester may withdraw their own pending request; nobody else may.
func TestIssueTransitionCancelIsRequesterOnly(t *testing.T) {
	transitionRule(t, testutil.Cols{
		"to_category":       "done",
		"allow_actor_types": testutil.Raw("ARRAY['member']::text[]"),
		"requires_approval": true,
	})
	issueID := dbfx.Issue(t, "F28 cancel", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_transition_request WHERE issue_id = $1`, issueID)
	})

	requester := memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	requesterID := requester.Header.Get("X-User-ID")
	var held map[string]any
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(requester, "id", issueID)).
		Want(http.StatusAccepted).JSON(&held)
	requestID, _ := held["request_id"].(string)

	stranger := memberRequest(t, "member", "DELETE", "/x", nil)
	testutil.Call(t, testHandler.CancelIssueTransitionRequest, withURLParam(stranger, "id", requestID)).
		Want(http.StatusForbidden)

	own := newRequest("DELETE", "/x", nil)
	own.Header.Set("X-User-ID", requesterID)
	testutil.Call(t, testHandler.CancelIssueTransitionRequest, withURLParam(own, "id", requestID)).
		Want(http.StatusNoContent)

	var state string
	dbfx.QueryRow(t, `SELECT state FROM issue_transition_request WHERE id = $1`, requestID).Scan(&state)
	if state != "cancelled" {
		t.Fatalf("state = %q, want cancelled", state)
	}
}

// The request history is readable from the issue.
func TestListIssueTransitionRequests(t *testing.T) {
	transitionRule(t, testutil.Cols{
		"to_category":       "done",
		"allow_actor_types": testutil.Raw("ARRAY['member']::text[]"),
		"requires_approval": true,
	})
	issueID := dbfx.Issue(t, "F28 history", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_transition_request WHERE issue_id = $1`, issueID)
	})
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(
		memberRequest(t, "member", "PUT", "/api/issues/"+issueID, map[string]any{"status": "done"}), "id", issueID)).
		Want(http.StatusAccepted)

	req := newRequest("GET", "/api/issues/"+issueID+"/transition-requests", nil)
	var body struct {
		Requests []IssueTransitionRequestResponse `json:"requests"`
	}
	testutil.Call(t, testHandler.ListIssueTransitionRequests, withURLParam(req, "id", issueID)).
		Want(http.StatusOK).JSON(&body)
	if len(body.Requests) != 1 || body.Requests[0].State != "pending" {
		raw, _ := json.Marshal(body.Requests)
		t.Fatalf("requests = %s, want one pending", raw)
	}
}
