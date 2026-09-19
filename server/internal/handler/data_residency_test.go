package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Data residency routing (K46). The policy decides where a run may execute;
// the per-runtime declaration says where each machine claims to be. The
// compliance matrix itself is tested in service/residency_test.go — these
// tests cover the endpoints and the one behaviour that only exists end to
// end: a policy with nowhere compliant to run REFUSES the enqueue rather than
// parking a task nobody may claim.

func setResidencyPolicyForTest(t *testing.T, policyJSON string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{data_residency_policy}', $2::jsonb) WHERE id = $1`, testWorkspaceID, policyJSON)
}

func declareRuntime(t *testing.T, runtimeID, region string, onPrem bool) {
	t.Helper()
	dbfx.Exec(t, `INSERT INTO runtime_compliance_profile (runtime_id, region, on_prem) VALUES ($1, $2, $3)
	              ON CONFLICT (runtime_id) DO UPDATE SET region = EXCLUDED.region, on_prem = EXCLUDED.on_prem`,
		runtimeID, region, onPrem)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM runtime_compliance_profile WHERE runtime_id = $1`, runtimeID)
	})
}

// TestDataResidencyPolicyEndpoint: the policy round-trips, normalizes on the
// way in, and is admin-only to write.
func TestDataResidencyPolicyEndpoint(t *testing.T) {
	rememberSettings(t)

	var got map[string]any
	testutil.Call(t, testHandler.GetDataResidencyPolicy, newRequest(http.MethodGet, "/api/data-residency", nil)).
		Want(http.StatusOK).JSON(&got)
	if regions, _ := got["region_allowlist"].([]any); len(regions) != 0 {
		t.Fatalf("a workspace with no policy must report no constraint, got %v", got["region_allowlist"])
	}
	if got["require_on_prem"] != false {
		t.Fatalf("require_on_prem defaults to false, got %v", got["require_on_prem"])
	}

	// Tokens are trimmed, lowercased, deduplicated and sorted on the way in,
	// so the stored policy is what RuntimeCompliant compares against directly.
	testutil.Call(t, testHandler.PutDataResidencyPolicy, newRequest(http.MethodPut, "/api/data-residency", map[string]any{
		"region_allowlist": []string{" EU-West-1 ", "eu-west-1", "", "af-south-1"},
		"banned_providers": []string{"Codex"},
		"require_on_prem":  true,
	})).Want(http.StatusOK).JSON(&got)
	regions, _ := got["region_allowlist"].([]any)
	if len(regions) != 2 || regions[0] != "af-south-1" || regions[1] != "eu-west-1" {
		t.Fatalf("region_allowlist = %v, want [af-south-1 eu-west-1]", got["region_allowlist"])
	}
	if providers, _ := got["banned_providers"].([]any); len(providers) != 1 || providers[0] != "codex" {
		t.Fatalf("banned_providers = %v, want [codex]", got["banned_providers"])
	}
	if got["require_on_prem"] != true {
		t.Fatalf("require_on_prem = %v, want true", got["require_on_prem"])
	}
	// The read endpoint returns what the write endpoint stored.
	var reread map[string]any
	testutil.Call(t, testHandler.GetDataResidencyPolicy, newRequest(http.MethodGet, "/api/data-residency", nil)).
		Want(http.StatusOK).JSON(&reread)
	if reread["require_on_prem"] != true {
		t.Fatalf("the stored policy must survive the round trip, got %v", reread)
	}

	// Over the cap: refused, not silently truncated.
	tooMany := make([]string, service.DataResidencyPolicyRange()+1)
	for i := range tooMany {
		tooMany[i] = "region-" + uuid.NewString()[:8]
	}
	testutil.Call(t, testHandler.PutDataResidencyPolicy, newRequest(http.MethodPut, "/api/data-residency", map[string]any{
		"region_allowlist": tooMany,
	})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutDataResidencyPolicy, newRequest(http.MethodPut, "/api/data-residency", "not an object")).
		Want(http.StatusBadRequest)

	// A plain member reads the policy but cannot change it: residency is a
	// workspace-wide compliance statement.
	plain := dbfx.User(t, "residency member", "residency-member-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, plain, "member")
	testutil.Call(t, testHandler.GetDataResidencyPolicy, newRequestAs(plain, http.MethodGet, "/api/data-residency", nil)).
		Want(http.StatusOK)
	testutil.Call(t, testHandler.PutDataResidencyPolicy, newRequestAs(plain, http.MethodPut, "/api/data-residency", map[string]any{
		"region_allowlist": []string{"us-east-1"},
	})).Want(http.StatusForbidden)
}

// TestRuntimeComplianceEndpoint: declaring, re-declaring and clearing one
// runtime's residency, and the gates around it.
func TestRuntimeComplianceEndpoint(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "residency rt "+uuid.NewString()[:8])
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM runtime_compliance_profile WHERE runtime_id = $1`, runtimeID)
	})
	call := func(req *http.Request) *http.Request {
		return withURLParam(req, "runtimeId", runtimeID)
	}

	var rt AgentRuntimeResponse
	testutil.Call(t, testHandler.PutRuntimeCompliance, call(newRequest(http.MethodPut, "/x", map[string]any{
		"region": " EU-West-1 ", "on_prem": true,
	}))).Want(http.StatusOK).JSON(&rt)
	if rt.Compliance == nil || rt.Compliance.Region != "eu-west-1" || !rt.Compliance.OnPrem {
		t.Fatalf("declaration = %+v, want eu-west-1 on-prem", rt.Compliance)
	}

	// Re-declaring overwrites rather than adding a second row: the unique
	// index from migration 702 is what makes this an upsert.
	testutil.Call(t, testHandler.PutRuntimeCompliance, call(newRequest(http.MethodPut, "/x", map[string]any{
		"region": "us-east-1", "on_prem": false,
	}))).Want(http.StatusOK).JSON(&rt)
	if rt.Compliance == nil || rt.Compliance.Region != "us-east-1" || rt.Compliance.OnPrem {
		t.Fatalf("re-declaration = %+v, want us-east-1 off-prem", rt.Compliance)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM runtime_compliance_profile WHERE runtime_id = $1`, runtimeID); n != 1 {
		t.Fatalf("a runtime holds one declaration, rows = %d", n)
	}

	// An empty region is not a declaration.
	testutil.Call(t, testHandler.PutRuntimeCompliance, call(newRequest(http.MethodPut, "/x", map[string]any{"region": "   "}))).
		Want(http.StatusBadRequest)

	// Clearing makes the runtime undeclared again — which under a restrictive
	// policy means ineligible, the fail-closed default.
	testutil.Call(t, testHandler.DeleteRuntimeCompliance, call(newRequest(http.MethodDelete, "/x", nil))).
		Want(http.StatusOK).JSON(&rt)
	if rt.Compliance != nil {
		t.Fatalf("a cleared declaration must serialize as null, got %+v", rt.Compliance)
	}

	// A plain member cannot declare on the workspace's behalf.
	plain := dbfx.User(t, "residency rt member", "residency-rt-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, plain, "member")
	testutil.Call(t, testHandler.PutRuntimeCompliance, call(newRequestAs(plain, http.MethodPut, "/x", map[string]any{"region": "eu-west-1"}))).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteRuntimeCompliance, call(newRequestAs(plain, http.MethodDelete, "/x", nil))).
		Want(http.StatusForbidden)

	// An unknown runtime is a 404, not a 500 or a silent no-op.
	unknown := withURLParam(newRequest(http.MethodPut, "/x", map[string]any{"region": "eu-west-1"}), "runtimeId", uuid.NewString())
	testutil.Call(t, testHandler.PutRuntimeCompliance, unknown).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.DeleteRuntimeCompliance, withURLParam(newRequest(http.MethodDelete, "/x", nil), "runtimeId", uuid.NewString())).
		Want(http.StatusNotFound)
}

// TestResidencyBlocksEnqueueWithoutACompliantRuntime is the whole point of the
// feature: with require_on_prem and only a cloud runtime, the run is refused
// before it is queued, the accountable humans hear about it, and the refusal
// is on the audit record with the policy that caused it.
func TestResidencyBlocksEnqueueWithoutACompliantRuntime(t *testing.T) {
	rememberSettings(t)
	ctx := context.Background()

	cloud := dbfx.Runtime(t, "cloud rt "+uuid.NewString()[:8], testutil.Cols{"runtime_mode": "cloud"})
	agent := dbfx.Agent(t, "residency agent "+uuid.NewString()[:8], cloud)
	issue := dbfx.Issue(t, "residency issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = $2`, testWorkspaceID, InboxTypeResidencyBlocked)
		testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE entity_id = $1`, agent)
	})

	setResidencyPolicyForTest(t, `{"region_allowlist":[],"banned_providers":[],"require_on_prem":true}`)
	_, err := enqueueForIssue(t, issue)
	if err == nil {
		t.Fatal("a cloud-only workspace under require_on_prem must refuse the enqueue")
	}
	if !containsAll(err.Error(), service.RoutingInvalidReason) {
		t.Fatalf("the refusal must name its reason, got %q", err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issue); n != 0 {
		t.Fatalf("a refused run must leave nothing in the queue, rows = %d", n)
	}
	if n := dbfx.Count(t,
		`SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2 AND details->>'code' = $3`,
		InboxTypeResidencyBlocked, issue, service.RoutingProblemResidencyNoCompliantRuntime); n < 1 {
		t.Fatal("a residency refusal must reach the accountable humans under its own inbox type")
	}
	// Its own type, not the generic routing alert: the reader's next step is
	// to declare a runtime or relax the policy, not to rebind the agent.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2`, InboxTypeRoutingAlert, issue); n != 0 {
		t.Fatalf("a residency refusal must not file a routing_alert, rows = %d", n)
	}
	if n := dbfx.Count(t,
		`SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2 AND details ? 'policy' AND details ? 'rejected'`,
		AuditResidencyDispatchBlocked, agent); n < 1 {
		t.Fatal("the refusal must be audited with the policy and the rejected runtimes")
	}

	// Declaring the same machine on-prem makes it eligible and the run queues
	// on it — the policy is a filter, not a ban on the workspace.
	dbfx.Exec(t, `UPDATE agent_runtime SET runtime_mode = 'local' WHERE id = $1`, cloud)
	declareRuntime(t, cloud, "eu-west-1", true)
	task, err := enqueueForIssue(t, issue)
	if err != nil {
		t.Fatalf("a compliant on-prem declaration must let the run queue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, uuidToString(task.ID)) })
	if uuidToString(task.RuntimeID) != cloud {
		t.Fatalf("the task must queue on the compliant runtime, got %s", uuidToString(task.RuntimeID))
	}
}

// TestResidencyRoutesAroundANonCompliantPoolMember: a policy the bound runtime
// fails is not fatal when the agent's pool holds a compliant member — the
// enqueue moves there exactly as it does for an offline machine.
func TestResidencyRoutesAroundANonCompliantPoolMember(t *testing.T) {
	rememberSettings(t)
	ctx := context.Background()

	banned := dbfx.Runtime(t, "banned rt "+uuid.NewString()[:8], testutil.Cols{"provider": "codex"})
	allowed := dbfx.Runtime(t, "allowed rt "+uuid.NewString()[:8], testutil.Cols{"provider": "claude"})
	pool := dbfx.Insert(t, "runtime_pool", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": "k46 " + uuid.NewString()[:8], "runtime_ids": `["` + allowed + `"]`,
	})
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM runtime_pool WHERE id = $1`, pool) })

	agent := dbfx.Agent(t, "pooled residency agent "+uuid.NewString()[:8], banned, testutil.Cols{"runtime_pool_id": pool})
	issue := dbfx.Issue(t, "pooled residency issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	setResidencyPolicyForTest(t, `{"region_allowlist":[],"banned_providers":["codex"],"require_on_prem":false}`)

	task, err := enqueueForIssue(t, issue)
	if err != nil {
		t.Fatalf("a compliant pool member must absorb the policy, not refuse the run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, uuidToString(task.ID)) })
	if uuidToString(task.RuntimeID) != allowed {
		t.Fatalf("the task must move to the compliant pool member, got %s", uuidToString(task.RuntimeID))
	}
}

type recordingTaskWakeup struct{ runtimeIDs []string }

func (r *recordingTaskWakeup) NotifyTaskAvailable(runtimeID, _ string) {
	r.runtimeIDs = append(r.runtimeIDs, runtimeID)
}

// A claim refused because the policy tightened after the enqueue puts the task
// back in the queue, but must not wake the runtime that was just refused: the
// daemon would claim the same task again at once, be refused again, and spin
// until the policy changes.
func TestResidencyClaimRefusalDoesNotWakeTheRefusedRuntime(t *testing.T) {
	rememberSettings(t)
	runtimeID := handlerTestRuntimeID(t)
	agent := dbfx.Agent(t, "residency claim agent "+uuid.NewString()[:8], runtimeID)
	issue := dbfx.Issue(t, "residency claim "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": runtimeID, "issue_id": issue})
	setResidencyPolicyForTest(t, `{"region_allowlist":[],"banned_providers":[],"require_on_prem":true}`)

	wakeup := &recordingTaskWakeup{}
	previous := testHandler.TaskService.Wakeup
	testHandler.TaskService.Wakeup = wakeup
	t.Cleanup(func() { testHandler.TaskService.Wakeup = previous })

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "residency-claim-daemon")
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(req, "runtimeId", runtimeID)).Want(http.StatusConflict)

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, task).Scan(&status)
	if status != "queued" {
		t.Fatalf("refused task status = %q, want queued", status)
	}
	for _, id := range wakeup.runtimeIDs {
		if id == runtimeID {
			t.Fatalf("the refused runtime %s was woken to claim the same task again", runtimeID)
		}
	}
}
