package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Trust Dial (K26): the mode only moves by a human's call and is logged;
// an observer cannot move an issue nor prove a criterion; propose waits for
// an approved plan; approval is today's behaviour; suggestions come from
// scorecards and are notified once a week, never applied.

func trustCall(t *testing.T, h http.HandlerFunc, method, path, agentID string, body any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, h, testutil.WithURLParams(newRequest(method, path, body), "id", agentID))
}

func agentMove(t *testing.T, issueID, agentID, status string) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{"status": status}), "X-Actor-Source", "task_token", "X-Agent-ID", agentID)
	return testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(req, "id", issueID))
}

func TestTrustDialModesGateAgentActions(t *testing.T) {
	agent := dbfx.Agent(t, "trust dial agent", handlerTestRuntimeID(t))
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM trust_mode_change WHERE agent_id = $1`, agent)
	})
	var mode struct {
		Mode  string   `json:"mode"`
		Modes []string `json:"modes"`
	}
	trustCall(t, testHandler.GetAgentTrustMode, http.MethodGet, "/api/agents/"+agent+"/trust-mode", agent, nil).Want(http.StatusOK).JSON(&mode)
	if mode.Mode != TrustPropose || len(mode.Modes) != 4 {
		t.Fatalf("new agent mode = %+v, want propose", mode)
	}

	issue := dbfx.Issue(t, "trust dial issue", testutil.Cols{"status": "todo", "assignee_type": "agent", "assignee_id": agent})
	// Propose without an approved plan: cannot start.
	res := agentMove(t, issue, agent, "in_progress")
	if res.Code != http.StatusForbidden || res.Map()["code"] != ErrCodeTrustModePlanRequired {
		t.Fatalf("propose without plan: %d %s", res.Code, res.Text())
	}
	// Backlog moves stay free.
	agentMove(t, issue, agent, "backlog").Want(http.StatusOK)
	// An approved (materialized) plan unlocks work.
	dbfx.Insert(t, "issue_plan", testutil.Cols{"workspace_id": testWorkspaceID, "issue_id": issue, "version": 1, "content": "plan", "steps": testutil.Raw("'[]'::jsonb"), "author_type": "agent", "author_id": agent, "materialized_at": testutil.Raw("now()")})
	agentMove(t, issue, agent, "in_progress").Want(http.StatusOK)

	// Observer: nothing moves, nothing is proved; the change is logged.
	trustCall(t, testHandler.SetAgentTrustMode, http.MethodPut, "/api/agents/"+agent+"/trust-mode", agent, map[string]any{"mode": "observer", "reason": "incident on billing"}).Want(http.StatusOK)
	trustCall(t, testHandler.SetAgentTrustMode, http.MethodPut, "/api/agents/"+agent+"/trust-mode", agent, map[string]any{"mode": "observer"}).Want(http.StatusUnprocessableEntity)
	trustCall(t, testHandler.SetAgentTrustMode, http.MethodPut, "/api/agents/"+agent+"/trust-mode", agent, map[string]any{"mode": "god"}).Want(http.StatusBadRequest)
	res = agentMove(t, issue, agent, "in_review")
	if res.Code != http.StatusForbidden || res.Map()["code"] != ErrCodeTrustModeBlocked {
		t.Fatalf("observer move: %d %s", res.Code, res.Text())
	}
	crit := criteriaOf(t, setCriteria(t, issue, []map[string]any{{"text": "Tests pass"}}))
	proveCriterion(t, issue, crit[0].ID, map[string]any{"proof_type": "test", "proof_ref": "go test"}, "X-Actor-Source", "task_token", "X-Agent-ID", agent).Want(http.StatusForbidden)
	// A human is never gated.
	moveIssue(t, issue, "in_review").Want(http.StatusOK)

	// Approval: today's behaviour.
	trustCall(t, testHandler.SetAgentTrustMode, http.MethodPut, "/api/agents/"+agent+"/trust-mode", agent, map[string]any{"mode": "approval"}).Want(http.StatusOK)
	agentMove(t, issue, agent, "in_progress").Want(http.StatusOK)

	var history struct {
		Changes []TrustModeChangeResponse `json:"changes"`
	}
	trustCall(t, testHandler.ListAgentTrustHistory, http.MethodGet, "/api/agents/"+agent+"/trust-mode/history", agent, nil).Want(http.StatusOK).JSON(&history)
	if len(history.Changes) != 2 || !history.Changes[1].Demotion || history.Changes[1].Reason == nil || *history.Changes[1].Reason != "incident on billing" || history.Changes[0].Demotion {
		t.Fatalf("history = %+v", history.Changes)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2`, AuditTrustModeChanged, agent); n != 2 {
		t.Fatalf("audit = %d, want 2", n)
	}
}

func TestTrustDialSuggestsFromScorecardsAndNotifiesOnce(t *testing.T) {
	agent := dbfx.Agent(t, "trust dial star", handlerTestRuntimeID(t))
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_scorecard_daily WHERE agent_id = $1`, agent)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'trust_promotion_suggested'`, testWorkspaceID)
	})
	var s TrustSuggestion
	trustCall(t, testHandler.GetAgentTrustSuggestion, http.MethodGet, "/api/agents/"+agent+"/trust-mode/suggestions", agent, nil).Want(http.StatusOK).JSON(&s)
	if s.Eligible || len(s.Reasons) == 0 {
		t.Fatalf("no runs must not be eligible: %+v", s)
	}
	for i := 0; i < 3; i++ {
		dbfx.Insert(t, "agent_scorecard_daily", testutil.Cols{
			"workspace_id": testWorkspaceID, "agent_id": agent, "runtime_id": handlerTestRuntimeID(t), "day": time.Now().AddDate(0, 0, -i).Format("2006-01-02"),
			"runs_total": 4, "runs_failed": 0, "runs_cancelled": 0, "runs_accepted": 4, "runs_reopened": 0, "runs_no_intervention": 3, "cost_usd_ticks_total": 0,
		})
	}
	trustCall(t, testHandler.GetAgentTrustSuggestion, http.MethodGet, "/api/agents/"+agent+"/trust-mode/suggestions", agent, nil).Want(http.StatusOK).JSON(&s)
	if !s.Eligible || s.SuggestedMode != TrustApproval || s.Metrics.RunsTotal != 12 || s.Metrics.AcceptedRate < 0.99 {
		t.Fatalf("suggestion = %+v, want eligible for approval", s)
	}
	if n, err := testHandler.NotifyTrustPromotions(t.Context(), time.Now()); err != nil || n < 1 {
		t.Fatalf("notify = %d, %v", n, err)
	}
	if n, err := testHandler.NotifyTrustPromotions(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("second notify = %d, %v; want 0 within the week", n, err)
	}
	if c := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = 'trust_promotion_suggested' AND details->>'agent_id' = $1 AND recipient_id = $2`, agent, testUserID); c != 1 {
		t.Fatalf("lead notices = %d, want 1", c)
	}
	var mode struct {
		Mode string `json:"mode"`
	}
	trustCall(t, testHandler.GetAgentTrustMode, http.MethodGet, "/api/agents/"+agent+"/trust-mode", agent, nil).Want(http.StatusOK).JSON(&mode)
	if mode.Mode != TrustPropose {
		t.Fatalf("a suggestion must never change the mode, got %s", mode.Mode)
	}
}
