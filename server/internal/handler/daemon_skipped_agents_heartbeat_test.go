package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestDaemonHeartbeat_UpdatesSkippedAgents pins the mid-session refresh: a CLI
// that breaks (or gets repaired) between two registrations must reach the
// runtime row from the heartbeat, because a long-lived daemon may not register
// again for hours. A beat that carries no map leaves the stored set alone; an
// empty map clears it.
func TestDaemonHeartbeat_UpdatesSkippedAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const daemonID = "test-daemon-skipped-heartbeat"
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE daemon_id = $1`, daemonID)
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "codex", "type": "codex", "version": "0.9.0", "status": "online"},
		},
	}, testWorkspaceID, daemonID)
	testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)

	var runtimeID string
	dbfx.QueryRow(t, `
		SELECT id::text FROM agent_runtime
		WHERE workspace_id = $1 AND daemon_id = $2 AND provider = 'codex'
	`, testWorkspaceID, daemonID).Scan(&runtimeID)

	heartbeat := func(body map[string]any) {
		t.Helper()
		hb := newDaemonTokenRequest("POST", "/api/daemon/heartbeat", body, testWorkspaceID, daemonID)
		testutil.Call(t, testHandler.DaemonHeartbeat, hb).Want(http.StatusOK)
	}

	// A CLI broke since the register: the heartbeat carries the new set, and
	// the same normalization registration uses drops the malformed entries.
	heartbeat(map[string]any{
		"runtime_id": runtimeID,
		"skipped_agents": map[string]any{
			"claude": "claude 1.0.3 rejected: minimum 2.1.0",
			"  ":     "orphan reason",
		},
	})
	meta := skippedAgentsMetadata(t, daemonID)
	if len(meta) != 1 || meta["claude"] != "claude 1.0.3 rejected: minimum 2.1.0" {
		t.Fatalf("skipped_agents after the heartbeat = %#v, want the single claude entry", meta)
	}

	// A beat with no map at all must not disturb what is stored — most beats
	// send nothing, and they must not read as "nothing is skipped".
	heartbeat(map[string]any{"runtime_id": runtimeID})
	if meta := skippedAgentsMetadata(t, daemonID); len(meta) != 1 {
		t.Fatalf("a silent heartbeat changed skipped_agents: %#v", meta)
	}

	// The CLI was repaired: an explicit empty map clears the diagnostic.
	heartbeat(map[string]any{
		"runtime_id":     runtimeID,
		"skipped_agents": map[string]any{},
	})
	if meta := skippedAgentsMetadata(t, daemonID); len(meta) != 0 {
		t.Fatalf("skipped_agents after the repair beat = %#v, want empty", meta)
	}
}
