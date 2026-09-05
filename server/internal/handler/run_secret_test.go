package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/permissionprofile"
)

// Run-scoped secrets (K09): scoped keys leave the claim as tokens, the run
// resolves them through its own credential only, any terminal status or an
// admin revokes them, a hidden key (K06) is never issued.

func bodyOf(t *testing.T, res *testutil.Response) string {
	t.Helper()
	raw, err := json.Marshal(res.Map())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestRunSecretsIssueResolveAndRevoke(t *testing.T) {
	issue, taskID, agentID := runningAgentRun(t, "run secret")
	dbfx.Exec(t, `UPDATE agent SET custom_env = '{"API_KEY":"real-value","PLAIN":"visible"}'::jsonb, scoped_env_keys = '["API_KEY","MISSING"]'::jsonb WHERE id = $1`, agentID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM run_scoped_secret WHERE task_id = $1`, taskID)
	})
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	env := testHandler.issueRunSecrets(context.Background(), task, agent, nil, map[string]string{"API_KEY": "real-value", "PLAIN": "visible"})
	token := env["API_KEY"]
	if !strings.HasPrefix(token, "mss_") || env["PLAIN"] != "visible" || len(env) != 2 {
		t.Fatalf("env = %v, want API_KEY tokenised and PLAIN untouched", env)
	}
	hdr := gateHeaders(taskID, agentID)
	// Listing never carries a value.
	if body := bodyOf(t, gateCall(t, testHandler.ListTaskRunSecrets, http.MethodGet, "/api/tasks/"+taskID+"/secrets", nil, hdr, "taskId", taskID).Want(http.StatusOK)); !strings.Contains(body, `"key":"API_KEY"`) || strings.Contains(body, "real-value") || strings.Contains(body, token) {
		t.Fatalf("listing leaked something: %s", body)
	}
	// Only the run resolves, and only its own token.
	testutil.Call(t, testHandler.ResolveRunSecret, testutil.WithURLParams(newRequest(http.MethodPost, "/api/tasks/"+taskID+"/secrets/resolve", map[string]any{"token": token}), "taskId", taskID)).Want(http.StatusForbidden)
	var resolved struct{ Key, Value string }
	gateCall(t, testHandler.ResolveRunSecret, http.MethodPost, "/api/tasks/"+taskID+"/secrets/resolve", map[string]any{"token": token}, hdr, "taskId", taskID).Want(http.StatusOK).JSON(&resolved)
	if resolved.Key != "API_KEY" || resolved.Value != "real-value" {
		t.Fatalf("resolved = %+v", resolved)
	}
	gateCall(t, testHandler.ResolveRunSecret, http.MethodPost, "/api/tasks/"+taskID+"/secrets/resolve", map[string]any{"token": "mss_nope"}, hdr, "taskId", taskID).Want(http.StatusNotFound)
	// Expiry is enforced even while the run continues.
	dbfx.Exec(t, `UPDATE run_scoped_secret SET expires_at = now() - interval '1 minute' WHERE task_id = $1`, taskID)
	if res := gateCall(t, testHandler.ResolveRunSecret, http.MethodPost, "/api/tasks/"+taskID+"/secrets/resolve", map[string]any{"token": token}, hdr, "taskId", taskID).Want(http.StatusForbidden); res.Map()["code"] != ErrCodeSecretExpired {
		t.Fatalf("expired token = %v", res.Map())
	}
	dbfx.Exec(t, `UPDATE run_scoped_secret SET expires_at = now() + interval '10 minute' WHERE task_id = $1`, taskID)
	// A member cannot revoke; an admin can; the run then gets an explicit refusal.
	viewer := dbfx.User(t, "Secret viewer", "secret-viewer@multica.ai")
	dbfx.Member(t, testWorkspaceID, viewer, "member")
	testutil.Call(t, testHandler.RevokeTaskRunSecrets, testutil.WithURLParams(testutil.WithHeaders(newRequest(http.MethodPost, "/api/tasks/"+taskID+"/secrets/revoke-all", nil), "X-User-ID", viewer), "taskId", taskID)).Want(http.StatusForbidden)
	var revoked struct{ Revoked int }
	testutil.Call(t, testHandler.RevokeTaskRunSecrets, testutil.WithURLParams(newRequest(http.MethodPost, "/api/tasks/"+taskID+"/secrets/revoke-all", nil), "taskId", taskID)).Want(http.StatusOK).JSON(&revoked)
	if revoked.Revoked != 1 {
		t.Fatalf("revoked = %d, want 1", revoked.Revoked)
	}
	if res := gateCall(t, testHandler.ResolveRunSecret, http.MethodPost, "/api/tasks/"+taskID+"/secrets/resolve", map[string]any{"token": token}, hdr, "taskId", taskID).Want(http.StatusForbidden); res.Map()["code"] != ErrCodeSecretRevoked {
		t.Fatalf("revoked token = %v", res.Map())
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action IN ($2, $3)`, taskID, AuditRunSecretIssued, AuditRunSecretRevoked); n != 2 {
		t.Fatalf("audit rows = %d, want issued + revoked", n)
	}
	// The issue view lists the run's secrets by status, still without values.
	if body := bodyOf(t, testutil.Call(t, testHandler.ListIssueRunSecrets, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/run-secrets", nil), "id", issue)).Want(http.StatusOK)); !strings.Contains(body, `"status":"revoked"`) || strings.Contains(body, "real-value") {
		t.Fatalf("issue listing = %s", body)
	}
}

func TestRunSecretsFollowProfileAndTerminalStatus(t *testing.T) {
	issue, taskID, agentID := runningAgentRun(t, "run secret profile")
	dbfx.Exec(t, `UPDATE agent SET custom_env = '{"PROD_KEY":"p","DEV_KEY":"d"}'::jsonb, scoped_env_keys = '["PROD_KEY","DEV_KEY"]'::jsonb WHERE id = $1`, agentID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM run_scoped_secret WHERE task_id = $1`, taskID)
	})
	task, _ := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	agent, _ := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	profile := &permissionprofile.Profile{Name: "code", HiddenSecrets: []string{"*PROD*"}}
	env := testHandler.issueRunSecrets(context.Background(), task, agent, profile, map[string]string{"PROD_KEY": "p", "DEV_KEY": "d"})
	if _, hidden := env["PROD_KEY"]; hidden || !strings.HasPrefix(env["DEV_KEY"], "mss_") {
		t.Fatalf("env = %v, want PROD_KEY dropped and DEV_KEY tokenised", env)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM run_scoped_secret WHERE task_id = $1`, taskID); n != 1 {
		t.Fatalf("issued rows = %d, want only DEV_KEY", n)
	}
	// Cancelling the run (a terminal status) revokes what is left.
	if err := testHandler.TaskService.CancelTasksForIssue(context.Background(), parseUUID(issue)); err != nil {
		t.Fatal(err)
	}
	var row db.RunScopedSecret
	if err := testPool.QueryRow(context.Background(), `SELECT revoked_at IS NOT NULL, revoke_reason FROM run_scoped_secret WHERE task_id = $1`, taskID).Scan(new(bool), &row.RevokeReason); err != nil {
		t.Fatal(err)
	}
	if row.RevokeReason.String != "run_cancelled" {
		t.Fatalf("revoke reason = %q, want run_cancelled", row.RevokeReason.String)
	}
}

// Scoped keys travel with the env PUT and come back on reveal.
func TestAgentEnvCarriesScopedKeys(t *testing.T) {
	agentID := dbfx.Agent(t, "scoped keys agent", handlerTestRuntimeID(t))
	var out AgentEnvResponse
	testutil.Call(t, testHandler.UpdateAgentEnv, testutil.WithURLParams(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", map[string]any{"custom_env": map[string]string{"A": "1", "B": "2"}, "scoped_keys": []string{" B ", "A", ""}}), "id", agentID)).Want(http.StatusOK).JSON(&out)
	if strings.Join(out.ScopedKeys, ",") != "A,B" {
		t.Fatalf("scoped keys = %v", out.ScopedKeys)
	}
	testutil.Call(t, testHandler.UpdateAgentEnv, testutil.WithURLParams(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", map[string]any{"custom_env": map[string]string{"A": "1"}}), "id", agentID)).Want(http.StatusOK).JSON(&out)
	if strings.Join(out.ScopedKeys, ",") != "A,B" {
		t.Fatal("an env PUT without scoped_keys must leave the list alone")
	}
	testutil.Call(t, testHandler.GetAgentEnv, testutil.WithURLParams(newRequest(http.MethodGet, "/api/agents/"+agentID+"/env", nil), "id", agentID)).Want(http.StatusOK).JSON(&out)
	if len(out.ScopedKeys) != 2 {
		t.Fatalf("reveal scoped keys = %v", out.ScopedKeys)
	}
}
