package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// F01 (JEF-5): the delegate is a SECOND actor pair on an issue naming the
// assignee's partner. It is deliberately inert — no run, no status — so what
// these tests pin is the pair contract (both halves together, member-or-agent
// only, never equal to the assignee) and the read paths that surface it.
//
// The "setting a delegate starts no run" half lives in
// internal/service/issue_trigger_delegate_test.go, beside the predicate that
// decides it.

// updateIssueFields runs PUT /api/issues/{id} with an arbitrary body so a test
// can send an explicit JSON null (which a typed struct cannot express) and read
// back the status the handler chose.
func updateIssueFields(t *testing.T, issueID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPut, "/api/issues/"+issueID, body)
	return testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(req, "id", issueID))
}

// readIssue re-reads the issue through the detail endpoint, so the assertions
// below are about what a client actually receives rather than about the row.
func readIssue(t *testing.T, issueID string) IssueResponse {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/issues/"+issueID, nil)
	return testutil.Decode[IssueResponse](t, testHandler.GetIssue, testutil.WithURLParams(req, "id", issueID), http.StatusOK)
}

func delegateTestMember(t *testing.T) string {
	t.Helper()
	suffix := time.Now().UnixNano()
	userID := dbfx.User(t, "Delegate Partner", fmt.Sprintf("delegate-partner-%d@multica.ai", suffix))
	dbfx.Member(t, testWorkspaceID, userID, "member")
	return userID
}

func TestIssueDelegateRoundTrip(t *testing.T) {
	issue := dbfx.Issue(t, "delegate round trip")
	partner := delegateTestMember(t)

	updateIssueFields(t, issue, map[string]any{
		"delegate_type": "member",
		"delegate_id":   partner,
	}).Want(http.StatusOK)

	got := readIssue(t, issue)
	if got.DelegateType == nil || *got.DelegateType != "member" {
		t.Fatalf("delegate_type = %v, want \"member\"", got.DelegateType)
	}
	if got.DelegateID == nil || *got.DelegateID != partner {
		t.Fatalf("delegate_id = %v, want %s", got.DelegateID, partner)
	}
	// The delegate must not have leaked into the assignee: they are separate
	// columns and separate meanings.
	if got.AssigneeType != nil || got.AssigneeID != nil {
		t.Fatalf("assignee = (%v,%v), want unset — a delegate write must not assign",
			got.AssigneeType, got.AssigneeID)
	}

	// Clearing sends both halves as explicit null, mirroring unassign.
	updateIssueFields(t, issue, map[string]any{
		"delegate_type": nil,
		"delegate_id":   nil,
	}).Want(http.StatusOK)
	if cleared := readIssue(t, issue); cleared.DelegateType != nil || cleared.DelegateID != nil {
		t.Fatalf("after clear: delegate = (%v,%v), want both nil", cleared.DelegateType, cleared.DelegateID)
	}
}

func TestIssueDelegateCreateAcceptsPair(t *testing.T) {
	partner := delegateTestMember(t)
	created := testutil.Decode[IssueResponse](t, testHandler.CreateIssue, newRequest(
		http.MethodPost, "/api/issues", map[string]any{
			"title":         "created with a delegate",
			"delegate_type": "member",
			"delegate_id":   partner,
		}), http.StatusCreated)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, created.ID)

	if created.DelegateID == nil || *created.DelegateID != partner {
		t.Fatalf("created delegate_id = %v, want %s", created.DelegateID, partner)
	}
	if reread := readIssue(t, created.ID); reread.DelegateID == nil || *reread.DelegateID != partner {
		t.Fatalf("re-read delegate_id = %v, want %s", reread.DelegateID, partner)
	}
}

// A half-pair is refused, and refused BEFORE writing: an issue that came out of
// a 400 holding half a delegate would be a row no validator ever accepted.
func TestIssueDelegateRejectsHalfPair(t *testing.T) {
	partner := delegateTestMember(t)
	for name, body := range map[string]map[string]any{
		"type only": {"delegate_type": "member"},
		"id only":   {"delegate_id": partner},
	} {
		t.Run(name, func(t *testing.T) {
			issue := dbfx.Issue(t, "half pair "+name)
			updateIssueFields(t, issue, body).Want(http.StatusBadRequest)

			got := readIssue(t, issue)
			if got.DelegateType != nil || got.DelegateID != nil {
				t.Fatalf("%s: rejected write still landed: delegate = (%v,%v)",
					name, got.DelegateType, got.DelegateID)
			}
		})
	}
}

// A squad is a routing object, not a partner. Refusing it here is what keeps
// the column's CHECK from being the thing that reports the mistake, as a 500.
func TestIssueDelegateRejectsSquad(t *testing.T) {
	leader := createHandlerTestAgent(t, fmt.Sprintf("Delegate Squad Leader %d", time.Now().UnixNano()), nil)
	squad := dbfx.Squad(t, fmt.Sprintf("DelegateSquad-%d", time.Now().UnixNano()), leader)
	issue := dbfx.Issue(t, "delegate squad refusal")

	updateIssueFields(t, issue, map[string]any{
		"delegate_type": "squad",
		"delegate_id":   squad,
	}).Want(http.StatusBadRequest)

	if got := readIssue(t, issue); got.DelegateType != nil {
		t.Fatalf("squad delegate was written: %v", *got.DelegateType)
	}
}

func TestIssueDelegateRejectsAssigneeAsDelegate(t *testing.T) {
	partner := delegateTestMember(t)
	issue := dbfx.Issue(t, "delegate equals assignee", testutil.Cols{
		"assignee_type": "member",
		"assignee_id":   partner,
	})

	// Same person as the existing assignee.
	updateIssueFields(t, issue, map[string]any{
		"delegate_type": "member",
		"delegate_id":   partner,
	}).Want(http.StatusBadRequest)

	// And the reverse direction: moving the ASSIGNEE onto the existing
	// delegate must be refused too, or the rule is trivially bypassed by
	// doing it in two writes.
	other := delegateTestMember(t)
	paired := dbfx.Issue(t, "assignee onto delegate", testutil.Cols{
		"assignee_type": "member",
		"assignee_id":   testUserID,
		"delegate_type": "member",
		"delegate_id":   other,
	})
	updateIssueFields(t, paired, map[string]any{
		"assignee_type": "member",
		"assignee_id":   other,
	}).Want(http.StatusBadRequest)
}

// A write that touches neither half must leave the delegate alone. UpdateIssue
// sends the pair as a bare narg, so an un-prefilled params struct would clear
// it — this is the regression that guards every caller's pre-fill.
func TestIssueDelegateSurvivesUnrelatedUpdate(t *testing.T) {
	partner := delegateTestMember(t)
	issue := dbfx.Issue(t, "delegate survives", testutil.Cols{
		"delegate_type": "member",
		"delegate_id":   partner,
	})

	updateIssueFields(t, issue, map[string]any{"priority": "high"}).Want(http.StatusOK)
	if got := readIssue(t, issue); got.DelegateID == nil || *got.DelegateID != partner {
		t.Fatalf("unrelated update cleared the delegate: %v", got.DelegateID)
	}

	// The description path takes the locked/atomic branch, which re-applies
	// the pre-fill through refreshUntouchedNullableIssueParams.
	updateIssueFields(t, issue, map[string]any{"description": "body"}).Want(http.StatusOK)
	if got := readIssue(t, issue); got.DelegateID == nil || *got.DelegateID != partner {
		t.Fatalf("atomic update cleared the delegate: %v", got.DelegateID)
	}
}

func TestIssueDelegateBatchUpdate(t *testing.T) {
	partner := delegateTestMember(t)
	a := dbfx.Issue(t, "batch delegate a")
	b := dbfx.Issue(t, "batch delegate b")

	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(
		http.MethodPost, "/api/issues/batch", map[string]any{
			"issue_ids": []string{a, b},
			"updates":   map[string]any{"delegate_type": "member", "delegate_id": partner},
		})).Want(http.StatusOK)

	for _, id := range []string{a, b} {
		if got := readIssue(t, id); got.DelegateID == nil || *got.DelegateID != partner {
			t.Fatalf("batch: issue %s delegate_id = %v, want %s", id, got.DelegateID, partner)
		}
	}
}

// listDelegateIssues runs the flat list with an arbitrary query fragment and
// returns the issue ids it produced.
func listDelegateIssues(t *testing.T, query string) map[string]bool {
	t.Helper()
	path := fmt.Sprintf("/api/issues?workspace_id=%s&limit=500%s", testWorkspaceID, query)
	var out struct {
		Issues []IssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.ListIssues, newRequest(http.MethodGet, path, nil)).
		Want(http.StatusOK).JSON(&out)
	ids := map[string]bool{}
	for _, i := range out.Issues {
		ids[i.ID] = true
	}
	return ids
}

func TestIssueDelegateFilters(t *testing.T) {
	partner := delegateTestMember(t)
	delegated := dbfx.Issue(t, "filtered delegated", testutil.Cols{
		"delegate_type": "member",
		"delegate_id":   partner,
	})
	plain := dbfx.Issue(t, "filtered plain")

	matched := listDelegateIssues(t, "&delegate_filters="+url.QueryEscape("member:"+partner))
	if !matched[delegated] {
		t.Fatalf("delegate_filters missed the delegated issue %s", delegated)
	}
	if matched[plain] {
		t.Fatalf("delegate_filters returned an issue with no delegate (%s)", plain)
	}

	none := listDelegateIssues(t, "&include_no_delegate=true")
	if !none[plain] {
		t.Fatalf("include_no_delegate missed the undelegated issue %s", plain)
	}
	if none[delegated] {
		t.Fatalf("include_no_delegate returned a delegated issue (%s)", delegated)
	}

	// The two combine as an OR, exactly like assignee_filters +
	// include_no_assignee: "these delegates, or nobody".
	both := listDelegateIssues(t, "&include_no_delegate=true&delegate_filters="+url.QueryEscape("member:"+partner))
	if !both[plain] || !both[delegated] {
		t.Fatalf("combined filter dropped one of %s / %s", plain, delegated)
	}
}

func TestIssueDelegateInvolvesUser(t *testing.T) {
	partner := delegateTestMember(t)
	delegated := dbfx.Issue(t, "involves delegate", testutil.Cols{
		"delegate_type": "member",
		"delegate_id":   partner,
	})
	unrelated := dbfx.Issue(t, "involves unrelated")

	got := listDelegateIssues(t, "&involves_user_id="+partner)
	if !got[delegated] {
		t.Fatalf("involves_user_id did not surface the issue %s the user is delegate on", delegated)
	}
	if got[unrelated] {
		t.Fatalf("involves_user_id surfaced an unrelated issue (%s)", unrelated)
	}
}

// Acceptance #4, end to end: naming an AGENT delegate must not queue a task.
// The unit-level statement of the same promise is
// internal/service/issue_trigger_delegate_test.go; this one proves the whole
// PUT flow, including that nothing downstream of WillEnqueueRun enqueues on
// its own.
func TestIssueDelegateAgentStartsNoRun(t *testing.T) {
	agent := seededReadyAgentID(t)
	issue := dbfx.Issue(t, "agent delegate starts no run")

	updateIssueFields(t, issue, map[string]any{
		"delegate_type": "agent",
		"delegate_id":   agent,
	}).Want(http.StatusOK)

	if n := taskCountFor(t, issue, agent); n != 0 {
		t.Fatalf("naming an agent delegate queued %d task(s); a delegate must never start a run", n)
	}
	// And the same write with the agent as ASSIGNEE does queue one, so the
	// assertion above is about the delegate rather than about a broken fixture.
	updateIssueFields(t, issue, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agent,
		"delegate_type": nil,
		"delegate_id":   nil,
	}).Want(http.StatusOK)
	if n := taskCountFor(t, issue, agent); n == 0 {
		t.Fatal("control case: assigning the same agent queued no task, so the delegate assertion proves nothing")
	}
}
