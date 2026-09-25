package handler

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestConcurrentSettingsPutsDoNotLoseEachOther covers the systemic audit
// finding: PUT handlers for data_residency_policy, drift, doc_drift,
// pr_walkthrough, competency, code_health, etc. used to read the whole
// workspace.settings blob, set their own top-level key, and write the whole
// blob back (UpdateWorkspace). Two concurrent PUTs on different keys could
// both read before either wrote, so whichever committed second silently
// erased the other's key (a classic lost update) — the exact bug
// MergeWorkspaceSettings (server/pkg/db/queries/workspace.sql) exists to
// close. This drives PutDataResidencyPolicy and PutDriftPolicy concurrently
// against the same workspace and asserts both keys survive.
func TestConcurrentSettingsPutsDoNotLoseEachOther(t *testing.T) {
	ctx := context.Background()
	workspaceID := dbfx.Workspace(t, "settings race", "settings-race-"+t.Name())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	residencyReq := func() *http.Request {
		req := newRequest(http.MethodPut, "/api/data-residency-policy", map[string]any{
			"region_allowlist": []string{"eu"}, "banned_providers": []string{}, "require_on_prem": false,
		})
		req.Header.Set("X-Workspace-ID", workspaceID)
		return req
	}
	driftReq := func() *http.Request {
		req := newRequest(http.MethodPut, "/api/drift-policy", map[string]any{
			"enabled": true, "repeated_action_threshold": 7, "file_reread_threshold": 9,
		})
		req.Header.Set("X-Workspace-ID", workspaceID)
		return req
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		testutil.Call(t, testHandler.PutDataResidencyPolicy, residencyReq()).Want(http.StatusOK)
	}()
	go func() {
		defer wg.Done()
		<-start
		testutil.Call(t, testHandler.PutDriftPolicy, driftReq()).Want(http.StatusOK)
	}()
	close(start)
	wg.Wait()

	var settings []byte
	dbfx.QueryRow(t, `SELECT settings FROM workspace WHERE id = $1`, workspaceID).Scan(&settings)
	body := string(settings)
	if !containsAll(body, `"data_residency_policy"`, `"eu"`) {
		t.Fatalf("settings after concurrent PUTs = %s; data_residency_policy was lost", body)
	}
	if !containsAll(body, `"drift"`, `"repeated_action_threshold": 7`) {
		t.Fatalf("settings after concurrent PUTs = %s; drift was lost", body)
	}
}
