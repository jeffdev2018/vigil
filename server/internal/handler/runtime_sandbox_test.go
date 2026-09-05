package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Sandbox mode per runtime (K10): the owner asks for a confinement, the
// daemon reports what the machine can do, the claim carries the request, and
// the start reports what the run got — a degradation is audited, never
// silent. The default stays none for every existing runtime.

func TestRuntimeSandbox(t *testing.T) {
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "sandbox runtime")
	patch := func(body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateAgentRuntime, withURLParam(newRequest(http.MethodPatch, "/api/runtimes/"+runtimeID, body), "runtimeId", runtimeID))
	}
	var rt AgentRuntimeResponse
	read := func() {
		var list []AgentRuntimeResponse
		testutil.Call(t, testHandler.ListAgentRuntimes, newRequest(http.MethodGet, "/api/runtimes", nil)).Want(http.StatusOK).JSON(&list)
		for _, x := range list {
			if x.ID == runtimeID {
				rt = x
			}
		}
	}
	read()
	if rt.SandboxMode != "none" || rt.SandboxEffective != "none" || len(rt.SandboxAllowedHosts) != 0 {
		t.Fatalf("default is none: %+v", rt)
	}
	patch(map[string]any{"sandbox_mode": "vm"}).Want(http.StatusBadRequest)
	patch(map[string]any{"sandbox_allowed_hosts": []string{"not a host"}}).Want(http.StatusBadRequest)
	patch(map[string]any{"sandbox_mode": "container", "sandbox_image": "ghcr.io/acme/agent:1", "sandbox_allowed_hosts": []string{" Api.Example.com ", ""}}).Want(http.StatusOK).JSON(&rt)
	if rt.SandboxMode != "container" || rt.SandboxImage != "ghcr.io/acme/agent:1" || len(rt.SandboxAllowedHosts) != 1 || rt.SandboxAllowedHosts[0] != "api.example.com" {
		t.Fatalf("saved: %+v", rt)
	}

	// The daemon reports what the machine can do.
	testutil.Call(t, testHandler.DaemonHeartbeat, newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{"runtime_id": runtimeID, "sandbox_capabilities": map[string]any{"os": "darwin", "docker": false, "bwrap": false, "modes": []string{"none"}}}, testWorkspaceID, "sandbox-daemon")).Want(http.StatusOK)
	read()
	if !strings.Contains(string(rt.SandboxCapabilities), `"docker":false`) {
		t.Fatalf("capabilities stored: %s", rt.SandboxCapabilities)
	}

	// The claim carries the request; the start reports the outcome.
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "sandbox agent")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})
	var claim struct {
		Task *struct {
			ID      string       `json:"id"`
			Sandbox *SandboxSpec `json:"sandbox"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "sandbox-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID || claim.Task.Sandbox == nil || claim.Task.Sandbox.Mode != "container" || claim.Task.Sandbox.Image != "ghcr.io/acme/agent:1" || claim.Task.Sandbox.AllowedHosts[0] != "api.example.com" {
		t.Fatalf("claim sandbox: %+v", claim.Task)
	}
	testutil.Call(t, testHandler.StartTask, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/start", map[string]any{"sandbox_requested": "container", "sandbox_mode": "none", "sandbox_reason": "docker is not available on this machine"}, testWorkspaceID, "sandbox-daemon"), "taskId", taskID)).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.started' AND details->>'sandbox_requested' = 'container' AND details->>'sandbox_mode' = 'none'`, taskID) != 1 {
		t.Fatal("the run snapshot records requested and effective confinement")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.sandbox_degraded' AND details->>'reason' LIKE 'docker is not%'`, taskID) != 1 {
		t.Fatal("a degradation is audited")
	}
	var effective string
	dbfx.QueryRow(t, `SELECT sandbox_effective FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&effective)
	if effective != "none" {
		t.Fatalf("runtime keeps the last effective mode: %s", effective)
	}
	// Turning the request off again is the default, so nothing is asked of the daemon.
	patch(map[string]any{"sandbox_mode": "none"}).Want(http.StatusOK).JSON(&rt)
	if rt.SandboxMode != "none" || rt.SandboxImage != "ghcr.io/acme/agent:1" {
		t.Fatalf("mode off keeps the image for later: %+v", rt)
	}
}
