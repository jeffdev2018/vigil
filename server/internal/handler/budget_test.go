package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestBudgetPolicyLifecycle(t *testing.T) {
	dbfx.Exec(t, `DELETE FROM budget_override WHERE workspace_id = $1`, testWorkspaceID)
	dbfx.Exec(t, `DELETE FROM budget_reservation WHERE policy_id IN (SELECT id FROM budget_policy WHERE workspace_id = $1)`, testWorkspaceID)
	dbfx.Exec(t, `DELETE FROM budget_period WHERE policy_id IN (SELECT id FROM budget_policy WHERE workspace_id = $1)`, testWorkspaceID)
	dbfx.Exec(t, `DELETE FROM budget_policy WHERE workspace_id = $1`, testWorkspaceID)

	create := newRequest(http.MethodPost, "/api/budgets", map[string]any{
		"scope_type": "workspace", "limit_usd_ticks": int64(50_000_000_000),
		"period": "monthly", "warn_bps": 8000, "action": "enforce",
	})
	w := testutil.Call(t, testHandler.CreateBudgetPolicy, create).Want(http.StatusCreated)
	var policy budgetPolicyResponse
	if err := json.NewDecoder(w.Body).Decode(&policy); err != nil {
		t.Fatal(err)
	}
	if policy.ScopeType != "workspace" || policy.Revision != 1 || policy.LimitUSDTicks != 50_000_000_000 {
		t.Fatalf("unexpected created policy: %+v", policy)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM budget_override WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM budget_reservation WHERE policy_id = $1`, policy.ID)
		testPool.Exec(context.Background(), `DELETE FROM budget_period WHERE policy_id = $1`, policy.ID)
		testPool.Exec(context.Background(), `DELETE FROM budget_policy WHERE id = $1`, policy.ID)
	})

	testutil.Call(t, testHandler.CreateBudgetPolicy, newRequest(http.MethodPost, "/api/budgets", map[string]any{
		"scope_type": "workspace", "limit_usd_ticks": int64(50_000_000_000),
		"period": "monthly", "warn_bps": 8000, "action": "enforce",
	})).Want(http.StatusConflict)
	testutil.Call(t, testHandler.CreateBudgetPolicy, newRequest(http.MethodPost, "/api/budgets", map[string]any{
		"scope_type": "project", "scope_id": "not-a-uuid", "limit_usd_ticks": 1,
		"period": "daily", "action": "enforce",
	})).Want(http.StatusBadRequest)

	w = testutil.Call(t, testHandler.ListBudgetPolicies, newRequest(http.MethodGet, "/api/budgets", nil)).Want(http.StatusOK)
	var policies []budgetPolicyResponse
	if err := json.NewDecoder(w.Body).Decode(&policies); err != nil || len(policies) != 1 {
		t.Fatalf("list policies = %+v, err = %v", policies, err)
	}

	update := withURLParam(newRequest(http.MethodPatch, "/api/budgets/"+policy.ID, map[string]any{
		"limit_usd_ticks": int64(75_000_000_000), "period": "weekly",
		"warn_bps": 9000, "action": "observe", "revision": policy.Revision,
	}), "id", policy.ID)
	w = testutil.Call(t, testHandler.UpdateBudgetPolicy, update).Want(http.StatusOK)
	var updated budgetPolicyResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Period != "weekly" || updated.Action != "observe" {
		t.Fatalf("unexpected updated policy: %+v", updated)
	}
	staleUpdate := withURLParam(newRequest(http.MethodPatch, "/api/budgets/"+policy.ID, map[string]any{
		"limit_usd_ticks": int64(75_000_000_000), "period": "weekly",
		"warn_bps": 9000, "action": "observe", "revision": policy.Revision,
	}), "id", policy.ID)
	testutil.Call(t, testHandler.UpdateBudgetPolicy, staleUpdate).Want(http.StatusConflict)

	statusReq := newRequest(http.MethodGet, "/api/budgets/status", nil)
	w = testutil.Call(t, testHandler.GetBudgetStatus, statusReq).Want(http.StatusOK)
	var statuses []budgetStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&statuses); err != nil || len(statuses) != 1 {
		t.Fatalf("budget status = %+v, err = %v", statuses, err)
	}
	if statuses[0].SpentUSDTicks != 0 || statuses[0].ReservedUSDTicks != 0 || statuses[0].Reached {
		t.Fatalf("unexpected initial status: %+v", statuses[0])
	}

	override := withURLParam(newRequest(http.MethodPost, "/api/budgets/"+policy.ID+"/override", map[string]any{
		"reason": "approved incident response", "duration_hours": 24,
	}), "id", policy.ID)
	w = testutil.Call(t, testHandler.CreateBudgetOverride, override).Want(http.StatusCreated)
	var granted struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(w.Body).Decode(&granted); err != nil {
		t.Fatal(err)
	}
	expires, err := time.Parse(time.RFC3339, granted.ExpiresAt)
	if err != nil || time.Until(expires) < 23*time.Hour || time.Until(expires) > 25*time.Hour {
		t.Fatalf("override expiry = %q, err = %v", granted.ExpiresAt, err)
	}

	del := withURLParam(newRequest(http.MethodDelete, "/api/budgets/"+policy.ID, nil), "id", policy.ID)
	testutil.Call(t, testHandler.DeleteBudgetPolicy, del).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.DeleteBudgetPolicy, del).Want(http.StatusNotFound)
}

func TestBudgetPolicyWriteRequiresManager(t *testing.T) {
	dbfx.Exec(t, `UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		// context.Background(): context.Background() is already cancelled inside Cleanup.
		testPool.Exec(context.Background(), `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	})
	testutil.Call(t, testHandler.CreateBudgetPolicy, newRequest(http.MethodPost, "/api/budgets", map[string]any{
		"scope_type": "workspace", "limit_usd_ticks": 1, "period": "daily", "action": "enforce",
	})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.ListBudgetPolicies, newRequest(http.MethodGet, "/api/budgets", nil)).Want(http.StatusOK)
}

// A server-side cancel settles the run's reservation before the daemon has
// stopped the agent; the daemon reports the tokens it burned right after.
// That report must still reach the period's spend, or cancelling a run just
// before it finishes spends money the cap never sees.
func TestReportTaskUsageAfterCancelChargesBudget(t *testing.T) {
	dbfx.Exec(t, `DELETE FROM budget_policy WHERE workspace_id = $1`, testWorkspaceID)
	policyID := dbfx.Insert(t, "budget_policy", testutil.Cols{
		"workspace_id": testWorkspaceID, "scope_type": "workspace",
		"limit_usd_ticks": int64(1_000_000_000_000), "period": "daily", "action": "enforce",
		"created_by": testUserID,
	})
	dbfx.Cleanup(t, `DELETE FROM budget_period WHERE policy_id = $1`, policyID)
	dbfx.Cleanup(t, `DELETE FROM budget_reservation WHERE policy_id = $1`, policyID)

	runtimeID := handlerTestRuntimeID(t)
	agentID := dbfx.Agent(t, "budget cancel agent", runtimeID)
	issueID := dbfx.Issue(t, "budget cancel", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID})
	var claim struct {
		Task *struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "budget-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID {
		t.Fatalf("claim = %+v, want task %s", claim.Task, taskID)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM budget_reservation WHERE task_id = $1 AND state = 'reserved'`, taskID); n != 1 {
		t.Fatalf("claimed task holds %d reservations, want 1", n)
	}

	if _, err := testHandler.TaskService.CancelTask(context.Background(), parseUUID(taskID)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	testutil.Call(t, testHandler.ReportTaskUsage, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/usage", map[string]any{"usage": []map[string]any{{"provider": "anthropic", "model": "claude-x", "input_tokens": 100, "output_tokens": 50, "cost_usd_ticks": 3_000_000}}}, testWorkspaceID, "budget-daemon"), "taskId", taskID)).Want(http.StatusOK)

	var spent, reserved int64
	dbfx.QueryRow(t, `SELECT COALESCE(SUM(spent_usd_ticks), 0)::bigint, COALESCE(SUM(reserved_usd_ticks), 0)::bigint FROM budget_period WHERE policy_id = $1`, policyID).Scan(&spent, &reserved)
	if spent != 3_000_000 || reserved != 0 {
		t.Fatalf("after cancel + late usage spent=%d reserved=%d, want 3000000 / 0", spent, reserved)
	}
}

// A direct chat send under an exhausted enforced cap is refused with the
// structured budget conflict, like every other admission path.
func TestSendChatMessageRefusedByExhaustedBudget(t *testing.T) {
	dbfx.Exec(t, `DELETE FROM budget_policy WHERE workspace_id = $1`, testWorkspaceID)
	policyID := dbfx.Insert(t, "budget_policy", testutil.Cols{
		"workspace_id": testWorkspaceID, "scope_type": "workspace",
		"limit_usd_ticks": int64(1), "period": "daily", "action": "enforce",
		"created_by": testUserID,
	})
	dbfx.Cleanup(t, `DELETE FROM budget_period WHERE policy_id = $1`, policyID)
	dbfx.Cleanup(t, `DELETE FROM budget_reservation WHERE policy_id = $1`, policyID)

	agentID := createHandlerTestAgent(t, "ChatBudgetAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	req := newRequest(http.MethodPost, "/api/chat-sessions/"+sessionID+"/messages", map[string]any{"content": "spend more"})
	req = withChatTestWorkspaceCtx(t, withURLParam(req, "sessionId", sessionID))
	var body struct {
		ReasonCode string `json:"reason_code"`
	}
	testutil.Call(t, testHandler.SendChatMessage, req).Want(http.StatusConflict).JSON(&body)
	if body.ReasonCode != string(ReasonBudgetExceeded) {
		t.Fatalf("reason_code = %q, want %q", body.ReasonCode, ReasonBudgetExceeded)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE chat_session_id = $1`, sessionID); n != 0 {
		t.Fatalf("refused send left %d tasks", n)
	}
}
