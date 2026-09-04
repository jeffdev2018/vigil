package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Approval gates (K05): a run's token opens a gate that is a Decision Card;
// the run polls; a human answer settles the gate without resuming the run;
// a deadline expires it; spend tokens are short-lived, capped and single-use.

func gateHeaders(taskID, agentID string) []string {
	return []string{"X-Actor-Source", "task_token", "X-Task-ID", taskID, "X-Agent-ID", agentID}
}

func gateCall(t *testing.T, h http.HandlerFunc, method, path string, body any, headers []string, params ...string) *testutil.Response {
	t.Helper()
	req := newRequest(method, path, body)
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	return testutil.Call(t, h, testutil.WithURLParams(req, params...))
}

func runningAgentRun(t *testing.T, label string) (issueID, taskID, agentID string) {
	t.Helper()
	agentID = dbfx.Agent(t, label+" agent", handlerTestRuntimeID(t))
	issueID = dbfx.Issue(t, label+" issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
	taskID = dbfx.Task(t, agentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issueID, "status": "running", "started_at": testutil.Raw("now()")})
	return
}

func TestApprovalGateAsksSettlesAndExpires(t *testing.T) {
	rememberSettings(t)
	issue, task, agent := runningAgentRun(t, "gate push")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM approval_gate_event WHERE task_id = $1`, task)
		testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	hdr := gateHeaders(task, agent)
	// Another run's token cannot open a gate here; a bad type is refused.
	gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+task+"/gates", map[string]any{"gate_type": "git_push", "summary": "push"}, gateHeaders(dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue}), agent), "taskId", task).Want(http.StatusForbidden)
	gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+task+"/gates", map[string]any{"gate_type": "rm_rf", "summary": "x"}, hdr, "taskId", task).Want(http.StatusBadRequest)

	var gate ApprovalGateResponse
	gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+task+"/gates", map[string]any{"gate_type": "git_push", "summary": "git push origin main", "details": map[string]any{"remote": "origin", "refs": []string{"main"}}}, hdr, "taskId", task).Want(http.StatusCreated).JSON(&gate)
	if gate.Status != "pending" || gate.DecisionID == nil || gate.GateType != "git_push" {
		t.Fatalf("gate = %+v", gate)
	}
	// The card carries the blocked action and two options.
	var cards struct{ Decisions []IssueDecisionResponse }
	testutil.Call(t, testHandler.ListIssueDecisions, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/decisions", nil), "id", issue)).Want(http.StatusOK).JSON(&cards)
	if len(cards.Decisions) != 1 || cards.Decisions[0].ID != *gate.DecisionID || len(cards.Decisions[0].Options) != 2 || cards.Decisions[0].Urgency != "high" {
		t.Fatalf("cards = %+v", cards.Decisions)
	}
	// Polling without an answer returns pending after the wait.
	var polled ApprovalGateResponse
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+gate.ID+"?wait=1", nil, hdr, "taskId", task, "gateId", gate.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "pending" {
		t.Fatalf("polled = %+v", polled)
	}
	// A human approves: the gate settles and no resume run is enqueued.
	before := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issue)
	respondDecision(t, issue, *gate.DecisionID, map[string]any{"option_id": "approve"}).Want(http.StatusOK)
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+gate.ID, nil, hdr, "taskId", task, "gateId", gate.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "approved" || polled.ResolvedAt == nil {
		t.Fatalf("after approval = %+v", polled)
	}
	if after := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issue); after != before {
		t.Fatalf("a gate answer must not enqueue a run: %d -> %d", before, after)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND details->>'gate_id' = $2`, AuditGateResolved, gate.ID); n != 1 {
		t.Fatalf("audit = %d", n)
	}
	// Deny.
	var denied ApprovalGateResponse
	gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+task+"/gates", map[string]any{"gate_type": "mcp_tool_call", "summary": "call stripe.refund"}, hdr, "taskId", task).Want(http.StatusCreated).JSON(&denied)
	respondDecision(t, issue, *denied.DecisionID, map[string]any{"option_id": "deny"}).Want(http.StatusOK)
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+denied.ID, nil, hdr, "taskId", task, "gateId", denied.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "denied" {
		t.Fatalf("after denial = %+v", polled)
	}
	// Expiry: a gate past its deadline is denied with reason timeout.
	var late ApprovalGateResponse
	gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+task+"/gates", map[string]any{"gate_type": "git_push", "summary": "push late"}, hdr, "taskId", task).Want(http.StatusCreated).JSON(&late)
	dbfx.Exec(t, `UPDATE approval_gate_event SET expires_at = now() - interval '1 minute' WHERE id = $1`, late.ID)
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+late.ID, nil, hdr, "taskId", task, "gateId", late.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "expired" {
		t.Fatalf("expired gate = %+v", polled)
	}
	// A workspace member reads the run's gates.
	var list struct{ Gates []ApprovalGateResponse }
	gateCall(t, testHandler.ListApprovalGates, http.MethodGet, "/api/tasks/"+task+"/gates", nil, nil, "taskId", task).Want(http.StatusOK).JSON(&list)
	if len(list.Gates) != 3 {
		t.Fatalf("gates = %d, want 3", len(list.Gates))
	}
}

func TestSpendTokensAreShortLivedCappedAndSingleUse(t *testing.T) {
	rememberSettings(t)
	issue, task, agent := runningAgentRun(t, "gate spend")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM approval_gate_event WHERE task_id = $1`, task)
		testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	hdr := gateHeaders(task, agent)
	// Under the $10 threshold: a token at once.
	var tok struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	gateCall(t, testHandler.IssueSpendToken, http.MethodPost, "/api/tasks/"+task+"/spend-token", map[string]any{"amount_usd_ticks": 20_000_000_000, "purpose": "openai"}, hdr, "taskId", task).Want(http.StatusOK).JSON(&tok)
	if tok.Token == "" || tok.ExpiresAt == "" {
		t.Fatalf("token = %+v", tok)
	}
	verify := func(token string, amount int64) *testutil.Response {
		return gateCall(t, testHandler.VerifySpendToken, http.MethodPost, "/api/tasks/"+task+"/spend-token/verify", map[string]any{"token": token, "amount_usd_ticks": amount}, hdr, "taskId", task)
	}
	if res := verify(tok.Token, 30_000_000_000); res.Code != http.StatusForbidden || res.Map()["code"] != "spend_over_cap" {
		t.Fatalf("over cap: %d %s", res.Code, res.Text())
	}
	verify(tok.Token, 20_000_000_000).Want(http.StatusOK)
	if res := verify(tok.Token, 1); res.Code != http.StatusForbidden || res.Map()["code"] != "spend_token_used" {
		t.Fatalf("second use: %d %s", res.Code, res.Text())
	}
	if res := verify("mst_nope", 1); res.Code != http.StatusForbidden || res.Map()["code"] != "spend_token_invalid" {
		t.Fatalf("unknown: %d %s", res.Code, res.Text())
	}
	// Expired: even a valid token is refused once past its time.
	gateCall(t, testHandler.IssueSpendToken, http.MethodPost, "/api/tasks/"+task+"/spend-token", map[string]any{"amount_usd_ticks": 10_000_000_000, "purpose": "old"}, hdr, "taskId", task).Want(http.StatusOK).JSON(&tok)
	dbfx.Exec(t, `UPDATE approval_gate_event SET details = details || '{"token_expires_at":"2000-01-01T00:00:00Z"}' WHERE task_id = $1 AND details->>'purpose' = 'old'`, task)
	if res := verify(tok.Token, 1); res.Code != http.StatusForbidden || res.Map()["code"] != "spend_token_expired" {
		t.Fatalf("expired: %d %s", res.Code, res.Text())
	}
	// Above the threshold: a spend gate; the token comes only after approval.
	var gate ApprovalGateResponse
	gateCall(t, testHandler.IssueSpendToken, http.MethodPost, "/api/tasks/"+task+"/spend-token", map[string]any{"amount_usd_ticks": 500_000_000_000, "purpose": "stripe"}, hdr, "taskId", task).Want(http.StatusAccepted).JSON(&gate)
	if gate.GateType != "spend" || gate.Status != "pending" {
		t.Fatalf("spend gate = %+v", gate)
	}
	gateCall(t, testHandler.IssueSpendToken, http.MethodPost, "/api/tasks/"+task+"/spend-token", map[string]any{"amount_usd_ticks": 500_000_000_000, "gate_id": gate.ID}, hdr, "taskId", task).Want(http.StatusAccepted)
	respondDecision(t, issue, *gate.DecisionID, map[string]any{"option_id": "approve"}).Want(http.StatusOK)
	gateCall(t, testHandler.IssueSpendToken, http.MethodPost, "/api/tasks/"+task+"/spend-token", map[string]any{"amount_usd_ticks": 500_000_000_000, "gate_id": gate.ID}, hdr, "taskId", task).Want(http.StatusOK).JSON(&tok)
	verify(tok.Token, 500_000_000_000).Want(http.StatusOK)
	gateCall(t, testHandler.IssueSpendToken, http.MethodPost, "/api/tasks/"+task+"/spend-token", map[string]any{"amount_usd_ticks": 1, "gate_id": gate.ID}, hdr, "taskId", task).Want(http.StatusConflict)
}
