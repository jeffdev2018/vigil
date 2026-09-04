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
		testPool.Exec(t.Context(), `DELETE FROM budget_override WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(t.Context(), `DELETE FROM budget_reservation WHERE policy_id = $1`, policy.ID)
		testPool.Exec(t.Context(), `DELETE FROM budget_period WHERE policy_id = $1`, policy.ID)
		testPool.Exec(t.Context(), `DELETE FROM budget_policy WHERE id = $1`, policy.ID)
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
		// context.Background(): t.Context() is already cancelled inside Cleanup.
		testPool.Exec(context.Background(), `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	})
	testutil.Call(t, testHandler.CreateBudgetPolicy, newRequest(http.MethodPost, "/api/budgets", map[string]any{
		"scope_type": "workspace", "limit_usd_ticks": 1, "period": "daily", "action": "enforce",
	})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.ListBudgetPolicies, newRequest(http.MethodGet, "/api/budgets", nil)).Want(http.StatusOK)
}
