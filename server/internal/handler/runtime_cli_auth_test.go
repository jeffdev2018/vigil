package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"

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

func TestGetCliAuthRequestRequiresManagerEvenForPublicRuntime(t *testing.T) {
	store, _ := withCliAuthStore(t)
	runtimeID, _, plainMemberID := runtimeVisibilityFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)
	created, err := store.Create(context.Background(), runtimeID, "login")
	if err != nil {
		t.Fatal(err)
	}
	req := withURLParams(newRequestAs(plainMemberID, http.MethodGet, "/api/runtimes/"+runtimeID+"/cli-auth/"+created.ID, nil), "runtimeId", runtimeID, "requestId", created.ID)
	testutil.Call(t, testHandler.GetCliAuthRequest, req).Want(http.StatusForbidden)
}

func TestCliAuthRejectsMachineCredentials(t *testing.T) {
	for _, source := range []string{"task_token", "cloud_pat"} {
		for _, tc := range []struct {
			method  string
			handler http.HandlerFunc
		}{
			{http.MethodPost, testHandler.InitiateCliAuth},
			{http.MethodDelete, testHandler.InitiateCliLogout},
			{http.MethodGet, testHandler.GetCliAuthRequest},
		} {
			req := newRequest(tc.method, "/api/runtimes/any/cli-auth", nil)
			req.Header.Set("X-Actor-Source", source)
			testutil.Call(t, tc.handler, req).Want(http.StatusForbidden)
		}
	}
}

func TestCliAuthCompletedProjectionRetriesAfterCommitFailureAndIgnoresOlderResults(t *testing.T) {
	store, _ := withCliAuthStore(t)
	runtimeID := cliAuthRuntime(t, "online")
	req, _ := store.Create(context.Background(), runtimeID, "login")
	original := testHandler.TxStarter
	testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	t.Cleanup(func() { testHandler.TxStarter = original })
	reportCliAuth(t, testWorkspaceID, runtimeID, req.ID, map[string]any{"status": "completed", "authenticated": true}).Want(http.StatusInternalServerError)
	completed, _ := store.Get(context.Background(), req.ID)
	if completed.Status != CliAuthCompleted {
		t.Fatalf("lost terminal decision: %+v", completed)
	}
	testHandler.TxStarter = original
	poll := withURLParams(newRequest(http.MethodGet, "/api/runtimes/"+runtimeID+"/cli-auth/"+req.ID, nil), "runtimeId", runtimeID, "requestId", req.ID)
	testutil.Call(t, testHandler.GetCliAuthRequest, poll).Want(http.StatusOK)
	// Even a duplicate contradictory report keeps the winning record.
	reportCliAuth(t, testWorkspaceID, runtimeID, req.ID, map[string]any{"status": "completed", "authenticated": false}).Want(http.StatusOK)
	rt, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(rt.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	var state struct {
		Authenticated bool   `json:"authenticated"`
		RequestID     string `json:"request_id"`
	}
	if err := json.Unmarshal(metadata["cli_auth"], &state); err != nil {
		t.Fatal(err)
	}
	if !state.Authenticated || state.RequestID != req.ID {
		t.Fatalf("wrong projection: %+v", state)
	}
	old := *completed
	old.ID = "older"
	old.UpdatedAt = completed.UpdatedAt.Add(-time.Second)
	no := false
	old.Authenticated = &no
	if err := testHandler.persistCliAuthState(context.Background(), rt, &old); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := testHandler.Queries.GetAgentRuntime(context.Background(), rt.ID)
	if string(unchanged.Metadata) != string(rt.Metadata) {
		t.Fatal("older projection overwrote current authentication")
	}
}

func TestCliAuthStateSurvivesRuntimeRegistration(t *testing.T) {
	for _, custom := range []bool{false, true} {
		ctx := context.Background()
		profileID := ""
		if custom {
			profileID = dbfx.Insert(t, "runtime_profile", testutil.Cols{"workspace_id": testWorkspaceID, "display_name": "Auth profile", "protocol_family": "codex", "command_name": "codex", "created_by": testUserID})
		}
		id := cliAuthRuntime(t, "online")
		dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id = $2, metadata = '{"cli_auth":{"authenticated":true},"offline_reason":{"code":"old"}}' WHERE id = $1`, id, "cli-auth-registration-"+id)
		if custom {
			dbfx.Exec(t, `UPDATE agent_runtime SET profile_id = $2 WHERE id = $1`, id, profileID)
		}
		rt, err := testHandler.Queries.GetAgentRuntime(ctx, parseUUID(id))
		if err != nil {
			t.Fatal(err)
		}
		fresh := []byte(`{"new_field":true,"cli_auth":{"authenticated":false}}`)
		var metadataBytes []byte
		if custom {
			registered, err := testHandler.Queries.UpsertAgentRuntimeWithProfile(ctx, db.UpsertAgentRuntimeWithProfileParams{
				WorkspaceID: rt.WorkspaceID, DaemonID: rt.DaemonID, Name: rt.Name, RuntimeMode: rt.RuntimeMode,
				Provider: rt.Provider, Status: "online", DeviceInfo: rt.DeviceInfo, Metadata: fresh, OwnerID: rt.OwnerID, ProfileID: parseUUID(profileID),
			})
			if err != nil {
				t.Fatal(err)
			}
			metadataBytes = registered.Metadata
		} else {
			registered, err := testHandler.Queries.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
				WorkspaceID: rt.WorkspaceID, DaemonID: rt.DaemonID, Name: rt.Name, RuntimeMode: rt.RuntimeMode,
				Provider: rt.Provider, Status: "online", DeviceInfo: rt.DeviceInfo, Metadata: fresh, OwnerID: rt.OwnerID,
			})
			if err != nil {
				t.Fatal(err)
			}
			metadataBytes = registered.Metadata
		}
		var metadata map[string]any
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata["new_field"] != true || metadata["offline_reason"] != nil {
			t.Fatalf("registration metadata not refreshed: %s", metadataBytes)
		}
		if metadata["cli_auth"].(map[string]any)["authenticated"] != true {
			t.Fatal("registration overwrote server authentication")
		}
	}
}
