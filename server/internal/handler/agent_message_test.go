package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Agent-to-agent messaging (F19 / JEF-32).
//
// The canonical assertions for the depth breaker live in a2a_depth_test.go and
// for the per-issue rate in a2a_budget_test.go. This file covers the endpoint's
// own contract: which intents it accepts, that the marker cannot be forged
// through the generic comment endpoint, and every refusal that is about WHO is
// speaking or WHO is being addressed.

// a2aFixture is one issue with two agents on the workspace's runtime and a live
// run for the sender.
type a2aFixture struct {
	IssueID  string
	SenderID string
	SenderTa string
	RecvID   string
}

func newA2AFixture(t *testing.T, label string) a2aFixture {
	t.Helper()
	runtimeID := handlerTestRuntimeID(t)
	issueID := dbfx.Issue(t, "a2a "+label)
	sender := dbfx.Agent(t, "a2a-sender-"+label, runtimeID)
	recv := dbfx.Agent(t, "a2a-recipient-"+label, runtimeID)
	task := dbfx.Task(t, sender, testutil.Cols{
		"issue_id":            issueID,
		"runtime_id":          runtimeID,
		"originator_user_id":  testUserID,
		"accountable_user_id": testUserID,
		"a2a_depth":           0,
	})
	return a2aFixture{IssueID: issueID, SenderID: sender, SenderTa: task, RecvID: recv}
}

// sendA2A posts one agent message as fromAgent speaking from fromTask.
func sendA2A(t *testing.T, issueID, fromAgent, fromTask string, body map[string]any) *testutil.Response {
	t.Helper()
	r := newRequest("POST", "/api/issues/"+issueID+"/agent-messages", body)
	r = withURLParam(r, "id", issueID)
	r.Header.Set("X-Agent-ID", fromAgent)
	r.Header.Set("X-Task-ID", fromTask)
	return testutil.Call(t, testHandler.SendAgentMessage, r)
}

func countCommentsWithIntent(t *testing.T, issueID string) int {
	t.Helper()
	var n int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1 AND a2a_intent IS NOT NULL`, issueID).Scan(&n)
	return n
}

// Acceptance 1: a valid message creates a comment, stamps the intent, and
// enqueues a run for the recipient.
func TestSendAgentMessage_CreatesCommentStampsIntentAndEnqueues(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newA2AFixture(t, "happy")

	var resp CommentResponse
	sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
		"to_agent_id": fx.RecvID,
		"intent":      "review",
		"body":        "please look at the migration",
	}).Want(http.StatusCreated).JSON(&resp)

	if resp.A2aIntent == nil || *resp.A2aIntent != "review" {
		t.Errorf("response a2a_intent = %v, want review", resp.A2aIntent)
	}
	// The body the sender wrote must survive verbatim, and the mention markup
	// must be the server's own — a caller never supplies it.
	if want := "mention://agent/" + fx.RecvID; !strings.Contains(resp.Content, want) {
		t.Errorf("content %q should carry the composed mention %q", resp.Content, want)
	}
	if !strings.Contains(resp.Content, "please look at the migration") {
		t.Errorf("content %q should carry the sender's body verbatim", resp.Content)
	}

	var stored string
	dbfx.QueryRow(t, `SELECT a2a_intent FROM comment WHERE id = $1`, resp.ID).Scan(&stored)
	if stored != "review" {
		t.Errorf("stored a2a_intent = %q, want review", stored)
	}

	var enqueued int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
		fx.IssueID, fx.RecvID).Scan(&enqueued)
	if enqueued != 1 {
		t.Errorf("recipient runs = %d, want 1 (trigger_outcomes=%v)", enqueued, resp.TriggerOutcomes)
	}
}

// ACCEPTANCE 2, the one the whole marker design rests on: the same body posted
// to the generic comment endpoint with an a2a_intent field must NOT write the
// intent. `type` is client-supplied there, which is exactly why the intent is a
// column no request field reaches rather than a comment_type_check value.
func TestCreateComment_IgnoresClientSuppliedA2AIntent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newA2AFixture(t, "forge")

	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/comments", map[string]any{
		"content":    "[@x](mention://agent/" + fx.RecvID + ")\n\nplease review",
		"a2a_intent": "review",
		// Try the type route too, in the same request: neither may land.
		"type": "comment",
	})
	r = withURLParam(r, "id", fx.IssueID)
	var resp CommentResponse
	testutil.Call(t, testHandler.CreateComment, r).Want(http.StatusCreated).JSON(&resp)

	if resp.A2aIntent != nil {
		t.Errorf("POST /comments must not echo an intent, got %q", *resp.A2aIntent)
	}
	var intent *string
	dbfx.QueryRow(t, `SELECT a2a_intent FROM comment WHERE id = $1`, resp.ID).Scan(&intent)
	if intent != nil {
		t.Fatalf("POST /comments wrote a2a_intent = %q; the marker is forgeable", *intent)
	}
	if got := countCommentsWithIntent(t, fx.IssueID); got != 0 {
		t.Fatalf("issue has %d intent-carrying comments after a forged post, want 0", got)
	}
}

func TestSendAgentMessage_RejectsUnknownIntent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newA2AFixture(t, "badintent")

	for _, intent := range []string{"", "gossip", "Review", "a2a_question"} {
		sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
			"to_agent_id": fx.RecvID,
			"intent":      intent,
			"body":        "hello",
		}).Want(http.StatusBadRequest)
	}
	if got := countCommentsWithIntent(t, fx.IssueID); got != 0 {
		t.Fatalf("a refused intent wrote %d comments, want 0", got)
	}
}

func TestSendAgentMessage_RejectsEmptyBody(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newA2AFixture(t, "emptybody")
	sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
		"to_agent_id": fx.RecvID, "intent": "question", "body": "   ",
	}).Want(http.StatusBadRequest)
}

// ACCEPTANCE 3, first half: a recipient the head-of-chain human cannot invoke,
// and a recipient that does not exist at all, must be INDISTINGUISHABLE. Any
// difference — status, reason code, or error string — turns this endpoint into
// an enumeration oracle for private agents.
func TestSendAgentMessage_NonInvocableAndUnknownRecipientAreIdentical(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newA2AFixture(t, "private")
	// A private agent owned by SOMEONE ELSE: visible to the workspace owner,
	// invocable by nobody but its owner.
	otherOwner := seedSecurityTestOwner(t, "a2a-private-owner")
	privateID := dbfx.Agent(t, "a2a-private-target", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id":        otherOwner,
		"permission_mode": "private",
	})

	body := func(to string) map[string]any {
		return map[string]any{"to_agent_id": to, "intent": "question", "body": "are you there"}
	}
	privateResp := sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, body(privateID)).Want(http.StatusForbidden)
	// A syntactically valid UUID that names nothing here.
	unknownResp := sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa,
		body("00000000-0000-4000-8000-00000000dead")).Want(http.StatusForbidden)

	if got := readReasonCode(t, privateResp.Body.Bytes()); got != "invocation_not_allowed" {
		t.Errorf("private recipient reason_code = %q, want invocation_not_allowed", got)
	}
	// Byte-identical bodies. Compared rather than each asserted separately,
	// because the property under test is that they cannot be told apart.
	if privateResp.Body.String() != unknownResp.Body.String() {
		t.Fatalf("private and unknown recipients are distinguishable:\n private: %s\n unknown: %s",
			privateResp.Body.String(), unknownResp.Body.String())
	}
	if got := countCommentsWithIntent(t, fx.IssueID); got != 0 {
		t.Fatalf("a refused message wrote %d comments, want 0", got)
	}
}

// ACCEPTANCE 3, second half: a caller whose own run has finished is refused. A
// terminal run lends nothing — and canInvokeAgent alone would not catch this for
// a workspace-public recipient, which still admits a workspace-internal agent
// principal, so the endpoint says no on the caller's own run state.
func TestSendAgentMessage_TerminalCallerRefused(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, status := range []string{"completed", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			fx := newA2AFixture(t, "terminal-"+status)
			dbfx.Exec(t, `UPDATE agent_task_queue SET status = $1 WHERE id = $2`, status, fx.SenderTa)
			sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
				"to_agent_id": fx.RecvID, "intent": "question", "body": "still here?",
			}).Want(http.StatusConflict)
			if got := countCommentsWithIntent(t, fx.IssueID); got != 0 {
				t.Fatalf("a %s caller wrote %d comments, want 0", status, got)
			}
		})
	}
}

func TestSendAgentMessage_RejectsSelfAddressing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newA2AFixture(t, "self")
	sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
		"to_agent_id": fx.SenderID, "intent": "question", "body": "talking to myself",
	}).Want(http.StatusBadRequest)
	if got := countCommentsWithIntent(t, fx.IssueID); got != 0 {
		t.Fatalf("self-addressing wrote %d comments, want 0", got)
	}
}

// A member calling this endpoint is refused rather than quietly accepted: a
// human-minted depth-1 run would charge ordinary traffic to the agent-loop
// budget and break "a human-triggered run always has depth 0".
func TestSendAgentMessage_RejectsMemberCaller(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newA2AFixture(t, "member")
	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/agent-messages", map[string]any{
		"to_agent_id": fx.RecvID, "intent": "question", "body": "hello",
	})
	r = withURLParam(r, "id", fx.IssueID)
	// No X-Agent-ID / X-Task-ID: this is the plain member shape.
	testutil.Call(t, testHandler.SendAgentMessage, r).Want(http.StatusBadRequest)
	if got := countCommentsWithIntent(t, fx.IssueID); got != 0 {
		t.Fatalf("member caller wrote %d comments, want 0", got)
	}
}

// ACCEPTANCE 9: a handoff wakes the recipient and declares the intent; it
// reassigns NOTHING. Ownership moves only when the recipient calls
// `multica issue assign`, which is guarded on its own.
func TestSendAgentMessage_HandoffDoesNotReassign(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newA2AFixture(t, "handoff")
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $1 WHERE id = $2`,
		fx.SenderID, fx.IssueID)

	sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
		"to_agent_id": fx.RecvID, "intent": "handoff", "body": "taking this over please",
	}).Want(http.StatusCreated)

	var assigneeType, assigneeID string
	dbfx.QueryRow(t, `SELECT assignee_type, assignee_id FROM issue WHERE id = $1`, fx.IssueID).
		Scan(&assigneeType, &assigneeID)
	if assigneeType != "agent" || assigneeID != fx.SenderID {
		t.Fatalf("handoff moved the assignee to (%s,%s); it must reassign nothing", assigneeType, assigneeID)
	}
}
