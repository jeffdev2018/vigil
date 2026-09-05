package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func cliAuthRuntime(t *testing.T, status string) string {
	t.Helper()
	return dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"daemon_id":    nil,
		"name":         "CLI auth runtime",
		"runtime_mode": "local",
		"provider":     "codex",
		"status":       status,
		"device_info":  "cli-auth.test",
		"metadata":     testutil.Raw(`'{"offline_reason":{"code":"test"}}'::jsonb`),
		"owner_id":     testUserID,
		"last_seen_at": testutil.Raw("now()"),
		"visibility":   "private",
	})
}

func withCliAuthStore(t *testing.T) (*InMemoryCliAuthStore, *pendingWorkRecorder) {
	t.Helper()
	oldStore, oldNotifier := testHandler.CliAuthStore, testHandler.DaemonPendingWork
	store := NewInMemoryCliAuthStore()
	notifier := &pendingWorkRecorder{}
	testHandler.CliAuthStore = store
	testHandler.DaemonPendingWork = notifier
	t.Cleanup(func() {
		testHandler.CliAuthStore = oldStore
		testHandler.DaemonPendingWork = oldNotifier
	})
	return store, notifier
}

func reportCliAuth(t *testing.T, workspaceID, runtimeID, requestID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost,
		"/api/daemon/runtimes/"+runtimeID+"/cli-auth/"+requestID+"/report",
		body, workspaceID, "cli-auth-daemon")
	req = withURLParams(req, "runtimeId", runtimeID, "requestId", requestID)
	return testutil.Call(t, testHandler.ReportCliAuthResult, req)
}

func TestInitiateCliAuthCreatesPendingWorkOnlyForOnlineRuntime(t *testing.T) {
	store, notifier := withCliAuthStore(t)
	onlineID := cliAuthRuntime(t, "online")
	req := withURLParam(newRequest(http.MethodPost, "/api/runtimes/"+onlineID+"/cli-auth", nil), "runtimeId", onlineID)
	var created CliAuthRequest
	testutil.Call(t, testHandler.InitiateCliAuth, req).Want(http.StatusOK).JSON(&created)
	if created.Status != CliAuthPending || created.Action != "login" {
		t.Fatalf("created request = %+v", created)
	}
	if notifier.count() != 1 || notifier.hints[0] != onlineID+":"+"cli_auth" {
		t.Fatalf("pending-work hints = %v", notifier.hints)
	}
	ack, _, err := testHandler.processHeartbeat(context.Background(), onlineID, false)
	if err != nil || ack.PendingCliAuth == nil || ack.PendingCliAuth.ID != created.ID || ack.PendingCliAuth.Action != "login" {
		t.Fatalf("heartbeat CLI auth = %+v, err=%v", ack.PendingCliAuth, err)
	}

	offlineID := cliAuthRuntime(t, "offline")
	offlineReq := withURLParam(newRequest(http.MethodPost, "/api/runtimes/"+offlineID+"/cli-auth", nil), "runtimeId", offlineID)
	testutil.Call(t, testHandler.InitiateCliAuth, offlineReq).Want(http.StatusServiceUnavailable)
	hasPending, err := store.HasPending(context.Background(), offlineID)
	if err != nil || hasPending {
		t.Fatalf("offline runtime created pending work: pending=%v err=%v", hasPending, err)
	}
}

func TestInitiateCliAuthRequiresRuntimeManager(t *testing.T) {
	_, _ = withCliAuthStore(t)
	runtimeID, _, plainMemberID := runtimeVisibilityFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public', provider = 'codex', status = 'online' WHERE id = $1`, runtimeID)
	req := withURLParam(newRequestAs(plainMemberID, http.MethodPost, "/api/runtimes/"+runtimeID+"/cli-auth", nil), "runtimeId", runtimeID)
	testutil.Call(t, testHandler.InitiateCliAuth, req).Want(http.StatusForbidden)
}

func TestInitiateCliLogoutQueuesLogoutAction(t *testing.T) {
	_, _ = withCliAuthStore(t)
	runtimeID := cliAuthRuntime(t, "online")
	req := withURLParam(newRequest(http.MethodDelete, "/api/runtimes/"+runtimeID+"/cli-auth", nil), "runtimeId", runtimeID)
	var created CliAuthRequest
	testutil.Call(t, testHandler.InitiateCliLogout, req).Want(http.StatusOK).JSON(&created)
	if created.Action != "logout" {
		t.Fatalf("action = %q, want logout", created.Action)
	}
}

func TestReportCliAuthPersistsStatusWithoutOverwritingMetadataAndIsIdempotent(t *testing.T) {
	store, _ := withCliAuthStore(t)
	runtimeID := cliAuthRuntime(t, "online")
	req, err := store.Create(context.Background(), runtimeID, "login")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PopPending(context.Background(), runtimeID); err != nil {
		t.Fatal(err)
	}

	reportCliAuth(t, testWorkspaceID, runtimeID, req.ID, map[string]any{
		"status": "running", "verification_url": "https://auth.openai.com/device", "user_code": "ABCD-EFGH",
	}).Want(http.StatusOK)
	progress, _ := store.Get(context.Background(), req.ID)
	if progress.VerificationURL == "" || progress.UserCode != "ABCD-EFGH" {
		t.Fatalf("progress not persisted: %+v", progress)
	}

	reportCliAuth(t, testWorkspaceID, runtimeID, req.ID, map[string]any{
		"status": "completed", "authenticated": true,
	}).Want(http.StatusOK)
	// A duplicate terminal report must be acknowledged but ignored.
	reportCliAuth(t, testWorkspaceID, runtimeID, req.ID, map[string]any{
		"status": "completed", "authenticated": false,
	}).Want(http.StatusOK)

	var metadata []byte
	dbfx.QueryRow(t, `SELECT metadata FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&metadata)
	var decoded map[string]any
	if err := json.Unmarshal(metadata, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["offline_reason"] == nil {
		t.Fatal("offline_reason was overwritten")
	}
	state, ok := decoded["cli_auth"].(map[string]any)
	if !ok || state["authenticated"] != true {
		t.Fatalf("cli_auth metadata = %#v", decoded["cli_auth"])
	}
}

func TestReportCliAuthRejectsWrongDaemonWorkspaceAndRedactsFailures(t *testing.T) {
	store, _ := withCliAuthStore(t)
	runtimeID := cliAuthRuntime(t, "online")
	req, _ := store.Create(context.Background(), runtimeID, "login")
	reportCliAuth(t, "00000000-0000-0000-0000-000000000000", runtimeID, req.ID, map[string]any{
		"status": "completed", "authenticated": true,
	}).Want(http.StatusNotFound)

	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz123456"
	reportCliAuth(t, testWorkspaceID, runtimeID, req.ID, map[string]any{
		"status": "failed", "error": "provider rejected " + secret,
	}).Want(http.StatusOK)
	stored, _ := store.Get(context.Background(), req.ID)
	if strings.Contains(stored.Error, secret) || !strings.Contains(stored.Error, "[REDACTED") {
		t.Fatalf("stored error was not redacted: %q", stored.Error)
	}
}
