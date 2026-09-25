package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/sandboxpolicy"
)

// Sandbox policies (JEF-256): the workspace < project < issue chain, its CRUD
// endpoints, and the claim-time merge into the confinement the claim carries.

type sandboxPolicyBody struct {
	Policy    *sandboxpolicy.Policy `json:"policy"`
	Override  *sandboxpolicy.Policy `json:"override"`
	Effective sandboxpolicy.Policy  `json:"effective"`
}

func createSandboxPolicyProject(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	var projectID string
	dbfx.QueryRow(t, `INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id`, testWorkspaceID, name).Scan(&projectID)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM project_sandbox_policy WHERE project_id = $1`, projectID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

func setWorkspaceSandboxPolicy(t *testing.T, policyJSON string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{sandbox_policy}', $2::jsonb) WHERE id = $1`, testWorkspaceID, policyJSON)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) - 'sandbox_policy' WHERE id = $1`, testWorkspaceID)
	})
}

func TestProjectSandboxPolicyCRUD(t *testing.T) {
	ctx := context.Background()
	projectID := createSandboxPolicyProject(t, ctx, "sandbox policy project")

	call := func(method string, body any) *testutil.Response {
		handlerFn := map[string]func(http.ResponseWriter, *http.Request){
			http.MethodGet:    testHandler.GetProjectSandboxPolicy,
			http.MethodPut:    testHandler.PutProjectSandboxPolicy,
			http.MethodDelete: testHandler.DeleteProjectSandboxPolicy,
		}[method]
		return testutil.Call(t, handlerFn, withURLParam(newRequest(method, "/api/projects/"+projectID+"/sandbox-policy", body), "id", projectID))
	}

	// Nothing stored: no policy, the effective layer is the default.
	var out sandboxPolicyBody
	call(http.MethodGet, nil).Want(http.StatusOK).JSON(&out)
	if out.Policy != nil || out.Effective.NetworkMode != "unrestricted" || len(out.Effective.AllowedHosts) != 0 || out.Effective.BlockSensitiveFiles {
		t.Fatalf("default: %+v", out)
	}

	// Validation: the mode is required and constrained, hosts only under
	// allowlist, and each host must pass the runtime's host rule.
	call(http.MethodPut, map[string]any{"allowed_hosts": []string{}}).Want(http.StatusBadRequest)
	call(http.MethodPut, map[string]any{"network_mode": "airgap"}).Want(http.StatusBadRequest)
	call(http.MethodPut, map[string]any{"network_mode": "none", "allowed_hosts": []string{"api.example.com"}}).Want(http.StatusBadRequest)
	call(http.MethodPut, map[string]any{"network_mode": "allowlist", "allowed_hosts": []string{"not a host"}}).Want(http.StatusBadRequest)

	// Store: hosts normalize like the runtime's (trim, lowercase, dedupe).
	call(http.MethodPut, map[string]any{"network_mode": "allowlist", "allowed_hosts": []string{" API.Example.com ", "api.example.com", ""}}).Want(http.StatusOK).JSON(&out)
	if out.Policy == nil || out.Policy.NetworkMode != "allowlist" || len(out.Policy.AllowedHosts) != 1 || out.Policy.AllowedHosts[0] != "api.example.com" {
		t.Fatalf("stored: %+v", out.Policy)
	}
	if out.Effective.NetworkMode != "allowlist" || len(out.Effective.AllowedHosts) != 1 {
		t.Fatalf("effective: %+v", out.Effective)
	}

	// The workspace default participates in the merge: most-restrictive wins,
	// so a workspace "none" outranks the project's allowlist, and the
	// sensitive-file block ORs in.
	setWorkspaceSandboxPolicy(t, `{"network_mode":"none","allowed_hosts":[],"block_sensitive_files":true}`)
	call(http.MethodGet, nil).Want(http.StatusOK).JSON(&out)
	if out.Policy == nil || out.Policy.NetworkMode != "allowlist" {
		t.Fatalf("the stored project layer is unaffected: %+v", out.Policy)
	}
	if out.Effective.NetworkMode != "none" || len(out.Effective.AllowedHosts) != 0 || !out.Effective.BlockSensitiveFiles {
		t.Fatalf("merged effective: %+v", out.Effective)
	}

	call(http.MethodDelete, nil).Want(http.StatusNoContent)
	call(http.MethodGet, nil).Want(http.StatusOK).JSON(&out)
	if out.Policy != nil {
		t.Fatalf("deleted: %+v", out.Policy)
	}
	// With the project layer gone the workspace default is the whole answer.
	if out.Effective.NetworkMode != "none" {
		t.Fatalf("effective inherits the workspace: %+v", out.Effective)
	}
}

func TestIssueSandboxOverrideCRUD(t *testing.T) {
	ctx := context.Background()
	projectID := createSandboxPolicyProject(t, ctx, "sandbox override project")
	issueID := dbfx.Issue(t, "sandbox override issue", testutil.Cols{"project_id": projectID})
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE issue SET metadata = metadata - 'sandbox_policy' WHERE id = $1`, issueID)
	})

	call := func(method string, body any) *testutil.Response {
		handlerFn := map[string]func(http.ResponseWriter, *http.Request){
			http.MethodGet:    testHandler.GetIssueSandboxOverride,
			http.MethodPut:    testHandler.PutIssueSandboxOverride,
			http.MethodDelete: testHandler.DeleteIssueSandboxOverride,
		}[method]
		return testutil.Call(t, handlerFn, withURLParam(newRequest(method, "/api/issues/"+issueID+"/sandbox-override", body), "id", issueID))
	}

	var out sandboxPolicyBody
	call(http.MethodGet, nil).Want(http.StatusOK).JSON(&out)
	if out.Override != nil || out.Effective.NetworkMode != "unrestricted" {
		t.Fatalf("default: %+v", out)
	}

	call(http.MethodPut, map[string]any{"network_mode": "bogus"}).Want(http.StatusBadRequest)
	call(http.MethodPut, map[string]any{"network_mode": "unrestricted", "allowed_hosts": []string{"api.example.com"}}).Want(http.StatusBadRequest)

	// The override never loosens the layers above it: the project allows one
	// host, the issue names two, the intersection keeps the shared one.
	testutil.Call(t, testHandler.PutProjectSandboxPolicy, withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/sandbox-policy", map[string]any{"network_mode": "allowlist", "allowed_hosts": []string{"api.example.com"}}), "id", projectID)).Want(http.StatusOK)
	call(http.MethodPut, map[string]any{"network_mode": "allowlist", "allowed_hosts": []string{"api.example.com", "cdn.example.com"}, "block_sensitive_files": true}).Want(http.StatusOK).JSON(&out)
	if out.Override == nil || out.Override.NetworkMode != "allowlist" || !out.Override.BlockSensitiveFiles {
		t.Fatalf("stored override: %+v", out.Override)
	}
	if out.Effective.NetworkMode != "allowlist" || len(out.Effective.AllowedHosts) != 1 || out.Effective.AllowedHosts[0] != "api.example.com" || !out.Effective.BlockSensitiveFiles {
		t.Fatalf("effective: %+v", out.Effective)
	}
	var raw []byte
	dbfx.QueryRow(t, `SELECT metadata->'sandbox_policy' FROM issue WHERE id = $1`, issueID).Scan(&raw)
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil || stored["network_mode"] != "allowlist" {
		t.Fatalf("metadata carries the override: %s", raw)
	}

	call(http.MethodDelete, nil).Want(http.StatusNoContent)
	call(http.MethodGet, nil).Want(http.StatusOK).JSON(&out)
	if out.Override != nil {
		t.Fatalf("deleted: %+v", out.Override)
	}
	// The project layer still applies after the override goes.
	if out.Effective.NetworkMode != "allowlist" || len(out.Effective.AllowedHosts) != 1 {
		t.Fatalf("effective falls back to the project layer: %+v", out.Effective)
	}
}

func TestClaimCarriesMergedSandboxPolicy(t *testing.T) {
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "sandbox policy runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "sandbox policy agent")
	projectID := createSandboxPolicyProject(t, ctx, "sandbox claim project")
	dbfx.Exec(t, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID)

	// The project asks for no network and the .env block; the runtime itself
	// asks for nothing (mode none). The claim must escalate to container, drop
	// the allowlist, and carry the block flag.
	testutil.Call(t, testHandler.PutProjectSandboxPolicy, withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/sandbox-policy", map[string]any{"network_mode": "none", "allowed_hosts": []string{}, "block_sensitive_files": true}), "id", projectID)).Want(http.StatusOK)

	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})
	var claim struct {
		Task *struct {
			ID      string       `json:"id"`
			Sandbox *SandboxSpec `json:"sandbox"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "sandbox-policy-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID {
		t.Fatalf("claim: %+v", claim.Task)
	}
	if claim.Task.Sandbox == nil || claim.Task.Sandbox.Mode != "container" || len(claim.Task.Sandbox.AllowedHosts) != 0 || !claim.Task.Sandbox.BlockSensitiveFiles {
		t.Fatalf("policy-merged sandbox: %+v", claim.Task.Sandbox)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.sandbox_policy_applied' AND details->>'network_mode' = 'none' AND (details->>'block_sensitive_files')::boolean`, taskID) != 1 {
		t.Fatal("the applied policy is audited")
	}

	// An issue override cannot loosen the project's no-network, but its own
	// restrictions merge in: allowlist under a none parent stays none.
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	dbfx.Exec(t, `UPDATE issue SET metadata = jsonb_set(metadata, '{sandbox_policy}', '{"network_mode":"allowlist","allowed_hosts":["api.example.com"],"block_sensitive_files":false}'::jsonb) WHERE id = $1`, issueID)
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE issue SET metadata = metadata - 'sandbox_policy' WHERE id = $1`, issueID)
	})
	taskID2 := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})
	claim.Task = nil
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "sandbox-policy-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID2 || claim.Task.Sandbox == nil || claim.Task.Sandbox.Mode != "container" || len(claim.Task.Sandbox.AllowedHosts) != 0 {
		t.Fatalf("override cannot loosen: %+v", claim.Task)
	}
}

func TestClaimSandboxPolicyNarrowsRuntimeAllowlist(t *testing.T) {
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "sandbox narrow runtime")
	dbfx.Exec(t, `UPDATE agent_runtime SET sandbox_mode = 'container', sandbox_allowed_hosts = '["api.example.com","logs.example.com"]'::jsonb WHERE id = $1`, runtimeID)
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "sandbox narrow agent")
	projectID := createSandboxPolicyProject(t, ctx, "sandbox narrow project")
	dbfx.Exec(t, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID)

	// The runtime allows two hosts, the policy one: the claim carries the
	// intersection, and the runtime's own mode is kept (already container).
	testutil.Call(t, testHandler.PutProjectSandboxPolicy, withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/sandbox-policy", map[string]any{"network_mode": "allowlist", "allowed_hosts": []string{"api.example.com"}}), "id", projectID)).Want(http.StatusOK)

	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})
	var claim struct {
		Task *struct {
			ID      string       `json:"id"`
			Sandbox *SandboxSpec `json:"sandbox"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "sandbox-narrow-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID || claim.Task.Sandbox == nil {
		t.Fatalf("claim: %+v", claim.Task)
	}
	if claim.Task.Sandbox.Mode != "container" || len(claim.Task.Sandbox.AllowedHosts) != 1 || claim.Task.Sandbox.AllowedHosts[0] != "api.example.com" {
		t.Fatalf("narrowed hosts: %+v", claim.Task.Sandbox)
	}
	if claim.Task.Sandbox.BlockSensitiveFiles {
		t.Fatalf("no layer sets the block: %+v", claim.Task.Sandbox)
	}
}

func TestClaimWithoutSandboxPolicyIsUnchanged(t *testing.T) {
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "sandbox default runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "sandbox default agent")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})
	var claim struct {
		Task *struct {
			ID      string       `json:"id"`
			Sandbox *SandboxSpec `json:"sandbox"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "sandbox-default-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID {
		t.Fatalf("claim: %+v", claim.Task)
	}
	if claim.Task.Sandbox != nil {
		t.Fatalf("no policy, no runtime request: sandbox must be absent, got %+v", claim.Task.Sandbox)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.sandbox_policy_applied'`, taskID) != 0 {
		t.Fatal("no policy, no audit event")
	}
}

// The wire shape is the frozen contract the front-end builds against.
func TestSandboxPolicyWireShape(t *testing.T) {
	raw, _ := json.Marshal(sandboxpolicy.Default())
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"network_mode", "allowed_hosts", "block_sensitive_files"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("default policy JSON misses %q: %s", key, raw)
		}
	}
}
