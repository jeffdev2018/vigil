package handler

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// BYOK model keys (K48): managers declare a key per vendor and scope, the
// value never comes back, a second active key conflicts unless it is a
// rotation, the claim injects the project key before the workspace key, usage
// is attributed to the key, and a vendor auth failure retires the key,
// alerts the managers and retries once on the next key.

func TestModelKeys(t *testing.T) {
	ctx := context.Background()
	prevBox := testHandler.ModelKeySecretBox
	t.Cleanup(func() { testHandler.ModelKeySecretBox = prevBox })
	testHandler.ModelKeySecretBox = nil
	ws := func(req *http.Request, more ...string) *http.Request {
		return testutil.WithURLParams(req, append([]string{"id", testWorkspaceID}, more...)...)
	}
	const secret = "sk-ant-api03-byoktestvalue-0000000000abcd"
	testutil.Call(t, testHandler.CreateModelKey, ws(newRequest(http.MethodPost, "/x", map[string]any{"scope": "workspace", "provider": "anthropic", "key": secret}))).Want(http.StatusConflict)
	box, err := secretbox.New(bytes.Repeat([]byte("k"), secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	testHandler.ModelKeySecretBox = box
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace_model_key WHERE workspace_id = $1`, testWorkspaceID) })

	member := dbfx.User(t, "byok member", "byok-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	testutil.Call(t, testHandler.CreateModelKey, ws(newRequestAs(member, http.MethodPost, "/x", map[string]any{"scope": "workspace", "provider": "anthropic", "key": secret}))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.CreateModelKey, ws(newRequest(http.MethodPost, "/x", map[string]any{"scope": "workspace", "provider": "acme", "key": secret}))).Want(http.StatusBadRequest)
	var key ModelKeyResponse
	res := testutil.Call(t, testHandler.CreateModelKey, ws(newRequest(http.MethodPost, "/x", map[string]any{"scope": "workspace", "provider": "anthropic", "key": secret, "label": "team"}))).Want(http.StatusCreated)
	res.JSON(&key)
	if strings.Contains(res.Body.String(), secret) || key.KeyHint != "sk-***abcd" || !key.Active {
		t.Fatalf("created: %s", res.Body.String())
	}
	var stored string
	dbfx.QueryRow(t, `SELECT key_encrypted FROM workspace_model_key WHERE id = $1`, key.ID).Scan(&stored)
	if strings.Contains(stored, secret) {
		t.Fatal("the key is encrypted at rest")
	}
	res = testutil.Call(t, testHandler.CreateModelKey, ws(newRequest(http.MethodPost, "/x", map[string]any{"scope": "workspace", "provider": "anthropic", "key": secret + "2"}))).Want(http.StatusConflict)
	if !strings.Contains(res.Body.String(), "model_key_active_conflict") {
		t.Fatalf("conflict code: %s", res.Body.String())
	}
	// Rotation keeps the old row, inactive.
	var rotated ModelKeyResponse
	testutil.Call(t, testHandler.RotateModelKey, ws(newRequest(http.MethodPost, "/x", map[string]any{"key": "sk-ant-api03-rotated-value-00000000wxyz"}), "keyId", key.ID)).Want(http.StatusCreated).JSON(&rotated)
	if rotated.KeyHint != "sk-***wxyz" || rotated.Label != "team" || dbfx.Count(t, `SELECT COUNT(*) FROM workspace_model_key WHERE id = $1 AND active = FALSE AND deactivated_reason = 'rotated'`, key.ID) != 1 {
		t.Fatalf("rotation: %+v", rotated)
	}
	// A project key outranks the workspace key for that project's runs.
	project := dbfx.Project(t, "byok project "+uuid.NewString()[:6])
	var projectKey ModelKeyResponse
	testutil.Call(t, testHandler.CreateModelKey, ws(newRequest(http.MethodPost, "/x", map[string]any{"scope": "project", "scope_id": project, "provider": "anthropic", "key": "sk-ant-api03-project-value-000000000proj", "label": "project"}))).Want(http.StatusCreated).JSON(&projectKey)
	testutil.Call(t, testHandler.CreateModelKey, ws(newRequest(http.MethodPost, "/x", map[string]any{"scope": "project", "scope_id": uuid.NewString(), "provider": "anthropic", "key": secret}))).Want(http.StatusBadRequest)

	runtimeID := dbfx.Runtime(t, "byok runtime", testutil.Cols{"provider": "claude"})
	agentID := dbfx.Agent(t, "byok agent "+uuid.NewString()[:6], runtimeID, testutil.Cols{"custom_env": `{"REGION":"eu"}`})
	issueID := dbfx.Issue(t, "byok issue "+uuid.NewString()[:6], testutil.Cols{"project_id": project, "assignee_type": "agent", "assignee_id": agentID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})
	var claim struct {
		Task *struct {
			ID    string `json:"id"`
			Agent *struct {
				CustomEnv map[string]string `json:"custom_env"`
			} `json:"agent"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "byok-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID || claim.Task.Agent == nil || claim.Task.Agent.CustomEnv["ANTHROPIC_API_KEY"] != "sk-ant-api03-project-value-000000000proj" || claim.Task.Agent.CustomEnv["REGION"] != "eu" {
		t.Fatalf("claim env: %+v", claim.Task)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE id = $1 AND model_key_id = $2`, taskID, projectKey.ID) != 1 {
		t.Fatal("the run is stamped with the key it spends")
	}
	// Usage is attributed to the key.
	testutil.Call(t, testHandler.ReportTaskUsage, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/usage", map[string]any{"usage": []map[string]any{{"provider": "anthropic", "model": "claude-x", "input_tokens": 100, "output_tokens": 50}}}, testWorkspaceID, "byok-daemon"), "taskId", taskID)).Want(http.StatusOK)
	var list struct {
		Keys  []ModelKeyResponse      `json:"keys"`
		Usage []ModelKeyUsageResponse `json:"usage"`
	}
	res = testutil.Call(t, testHandler.ListModelKeys, ws(newRequestAs(member, http.MethodGet, "/x", nil))).Want(http.StatusOK)
	res.JSON(&list)
	if strings.Contains(res.Body.String(), "sk-ant-api03") || len(list.Keys) != 3 || len(list.Usage) != 1 || list.Usage[0].ModelKeyID != projectKey.ID || list.Usage[0].InputTokens != 100 {
		t.Fatalf("list: %s", res.Body.String())
	}
	// A vendor auth failure retires the project key, alerts the managers and retries on the workspace key.
	testutil.Call(t, testHandler.FailTask, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/fail", map[string]any{"error": "401 authentication_error: invalid x-api-key", "failure_reason": "agent_error.provider_auth_or_access"}, testWorkspaceID, "byok-daemon"), "taskId", taskID)).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM workspace_model_key WHERE id = $1 AND active = FALSE AND deactivated_reason = 'agent_error.provider_auth_or_access'`, projectKey.ID) != 1 {
		t.Fatal("the failing key is retired with the reason")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'model_key_alert' AND details->>'key_id' = $2 AND details->>'failover' = 'true'`, testWorkspaceID, projectKey.ID) < 1 {
		t.Fatal("managers are alerted on the first failover")
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'model_key_alert'`, testWorkspaceID)
	})
	var retryID string
	dbfx.QueryRow(t, `SELECT id::text FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued' AND id <> $2 ORDER BY created_at DESC LIMIT 1`, issueID, taskID).Scan(&retryID)
	if retryID == "" {
		t.Fatal("a retry is enqueued on the next key")
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, retryID) })
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "byok-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != retryID || claim.Task.Agent.CustomEnv["ANTHROPIC_API_KEY"] != "sk-ant-api03-rotated-value-00000000wxyz" {
		t.Fatalf("the retry spends the workspace key: %+v", claim.Task)
	}
	// Failing again with no key left: retired, alerted, no retry.
	testutil.Call(t, testHandler.FailTask, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+retryID+"/fail", map[string]any{"error": "429 quota exceeded", "failure_reason": "agent_error.provider_quota_limit"}, testWorkspaceID, "byok-daemon"), "taskId", retryID)).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM workspace_model_key WHERE workspace_id = $1 AND active = TRUE`, testWorkspaceID) != 0 || dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, issueID) != 0 {
		t.Fatal("no key left: nothing retried")
	}
	// Retiring by hand; a retired key is not retired twice.
	var fresh ModelKeyResponse
	testutil.Call(t, testHandler.CreateModelKey, ws(newRequest(http.MethodPost, "/x", map[string]any{"scope": "workspace", "provider": "openai", "key": "sk-proj-openai-value-000000000000zz99"}))).Want(http.StatusCreated).JSON(&fresh)
	testutil.Call(t, testHandler.DeactivateModelKey, ws(newRequest(http.MethodDelete, "/x", nil), "keyId", fresh.ID)).Want(http.StatusOK)
	testutil.Call(t, testHandler.DeactivateModelKey, ws(newRequest(http.MethodDelete, "/x", nil), "keyId", fresh.ID)).Want(http.StatusNotFound)
}
