package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/permissionprofile"
)

// Permission profiles (K06): seeded per workspace, editable by admins,
// carried by an agent, overridable per run, enforced at the gates.
// Pure glob/secret/prompt logic: server/pkg/permissionprofile.

func profileCall(t *testing.T, h http.HandlerFunc, method, path string, body any, params ...string) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(newRequest(method, path, body), "X-Workspace-ID", testWorkspaceID)
	return testutil.Call(t, h, testutil.WithURLParams(req, params...))
}

func listProfiles(t *testing.T) map[string]permissionprofile.Profile {
	t.Helper()
	var out struct {
		Profiles []permissionprofile.Profile `json:"profiles"`
	}
	profileCall(t, testHandler.ListPermissionProfiles, http.MethodGet, "/api/permission-profiles", nil).Want(http.StatusOK).JSON(&out)
	byName := map[string]permissionprofile.Profile{}
	for _, p := range out.Profiles {
		byName[p.Name] = p
	}
	return byName
}

func cleanupProfiles(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_permission_profile WHERE workspace_id = $1`, testWorkspaceID)
	})
}

func TestPermissionProfilesSeedEditAndAssign(t *testing.T) {
	cleanupProfiles(t)
	profiles := listProfiles(t)
	for _, name := range []string{"read_only", "code", "ci", "staging", "production"} {
		if p, ok := profiles[name]; !ok || !p.Builtin {
			t.Fatalf("builtin %s must be seeded: %+v", name, profiles)
		}
	}
	if again := listProfiles(t); len(again) != len(profiles) {
		t.Fatal("a second read must not seed twice")
	}
	code := profiles["code"]

	// Rules: a bad glob is refused, a good one lands and is audited.
	profileCall(t, testHandler.UpdatePermissionProfile, http.MethodPatch, "/api/permission-profiles/"+code.ID, map[string]any{"denied_paths": []string{"[a]"}}, "id", code.ID).Want(http.StatusBadRequest)
	var updated permissionprofile.Profile
	profileCall(t, testHandler.UpdatePermissionProfile, http.MethodPatch, "/api/permission-profiles/"+code.ID, map[string]any{"denied_paths": []string{"secrets/**"}, "hidden_secrets": []string{"*_PASSWORD"}}, "id", code.ID).Want(http.StatusOK).JSON(&updated)
	if len(updated.DeniedPaths) != 1 || updated.DeniedPaths[0] != "secrets/**" || len(updated.AllowedCommands) != 1 || updated.HiddenSecrets[0] != "*_PASSWORD" {
		t.Fatalf("updated = %+v, want denied_paths and hidden_secrets replaced, allowed_commands kept", updated)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2`, AuditPermissionProfileChanged, code.ID); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
	// Members may read, not write.
	viewer := dbfx.User(t, "Profile viewer", "profile-viewer@multica.ai")
	dbfx.Member(t, testWorkspaceID, viewer, "member")
	req := testutil.WithHeaders(newRequest(http.MethodPatch, "/api/permission-profiles/"+code.ID, map[string]any{"read_only": true}), "X-Workspace-ID", testWorkspaceID, "X-User-ID", viewer)
	testutil.Call(t, testHandler.UpdatePermissionProfile, testutil.WithURLParams(req, "id", code.ID)).Want(http.StatusForbidden)

	// Custom profiles: created, unique by name, deletable only when unused; builtins never.
	var custom permissionprofile.Profile
	profileCall(t, testHandler.CreatePermissionProfile, http.MethodPost, "/api/permission-profiles", map[string]any{"name": "docs", "read_only": true, "denied_paths": []string{"server/**"}}).Want(http.StatusCreated).JSON(&custom)
	profileCall(t, testHandler.CreatePermissionProfile, http.MethodPost, "/api/permission-profiles", map[string]any{"name": "docs"}).Want(http.StatusConflict)
	profileCall(t, testHandler.DeletePermissionProfile, http.MethodDelete, "/api/permission-profiles/"+code.ID, nil, "id", code.ID).Want(http.StatusBadRequest)

	// Assignment: the agent carries the profile; a member who does not own it cannot change it.
	agent := dbfx.Agent(t, "profile agent", handlerTestRuntimeID(t))
	var resp AgentResponse
	profileCall(t, testHandler.SetAgentPermissionProfile, http.MethodPut, "/api/agents/"+agent+"/permission-profile", map[string]any{"profile_id": custom.ID}, "id", agent).Want(http.StatusOK).JSON(&resp)
	if resp.PermissionProfileID == nil || *resp.PermissionProfileID != custom.ID {
		t.Fatalf("agent profile = %v, want %s", resp.PermissionProfileID, custom.ID)
	}
	profileCall(t, testHandler.DeletePermissionProfile, http.MethodDelete, "/api/permission-profiles/"+custom.ID, nil, "id", custom.ID).Want(http.StatusConflict)
	req = testutil.WithHeaders(newRequest(http.MethodPut, "/api/agents/"+agent+"/permission-profile", map[string]any{"profile_id": nil}), "X-Workspace-ID", testWorkspaceID, "X-User-ID", viewer)
	testutil.Call(t, testHandler.SetAgentPermissionProfile, testutil.WithURLParams(req, "id", agent)).Want(http.StatusForbidden)
	profileCall(t, testHandler.SetAgentPermissionProfile, http.MethodPut, "/api/agents/"+agent+"/permission-profile", map[string]any{"profile_id": nil}, "id", agent).Want(http.StatusOK)
	profileCall(t, testHandler.DeletePermissionProfile, http.MethodDelete, "/api/permission-profiles/"+custom.ID, nil, "id", custom.ID).Want(http.StatusNoContent)
}

func TestPermissionProfileResolvesPerRunAndDeniesGates(t *testing.T) {
	cleanupProfiles(t)
	profiles := listProfiles(t)
	issue, task, agent := runningAgentRun(t, "profile gate")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM approval_gate_event WHERE task_id = $1`, task)
		testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	type resolved struct {
		Profile *permissionprofile.Profile `json:"profile"`
		Source  string                     `json:"source"`
	}
	get := func() resolved {
		var out resolved
		profileCall(t, testHandler.GetTaskPermissionProfile, http.MethodGet, "/api/tasks/"+task+"/permission-profile", nil, "taskId", task).Want(http.StatusOK).JSON(&out)
		return out
	}
	if r := get(); r.Profile != nil || r.Source != "none" {
		t.Fatalf("no profile yet: %+v", r)
	}
	profileCall(t, testHandler.SetAgentPermissionProfile, http.MethodPut, "/api/agents/"+agent+"/permission-profile", map[string]any{"profile_id": profiles["code"].ID}, "id", agent).Want(http.StatusOK)
	if r := get(); r.Profile == nil || r.Profile.Name != "code" || r.Source != "agent" {
		t.Fatalf("agent profile must resolve: %+v", r)
	}
	// The run itself may not change its permissions; an admin may.
	gateCall(t, testHandler.SetTaskPermissionProfile, http.MethodPut, "/api/tasks/"+task+"/permission-profile", map[string]any{"profile_id": profiles["production"].ID}, gateHeaders(task, agent), "taskId", task).Want(http.StatusForbidden)
	profileCall(t, testHandler.SetTaskPermissionProfile, http.MethodPut, "/api/tasks/"+task+"/permission-profile", map[string]any{"profile_id": profiles["read_only"].ID}, "taskId", task).Want(http.StatusOK)
	if r := get(); r.Profile == nil || r.Profile.Name != "read_only" || r.Source != "task" {
		t.Fatalf("task override must win: %+v", r)
	}

	// Gates: the profile refuses before any card is filed.
	open := func(gateType string, paths []string) ApprovalGateResponse {
		var g ApprovalGateResponse
		gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+task+"/gates", map[string]any{"gate_type": gateType, "summary": "x", "details": map[string]any{"paths": paths}}, gateHeaders(task, agent), "taskId", task).Want(http.StatusCreated).JSON(&g)
		return g
	}
	if g := open("git_push", []string{"docs/a.md"}); g.Status != "denied" || g.DecisionID != nil {
		t.Fatalf("read_only run pushing = %+v, want denied without a card", g)
	}
	profileCall(t, testHandler.SetTaskPermissionProfile, http.MethodPut, "/api/tasks/"+task+"/permission-profile", map[string]any{"profile_id": nil}, "taskId", task).Want(http.StatusOK)
	if g := open("mcp_tool_call", []string{"src/a.ts", ".env.local"}); g.Status != "denied" {
		t.Fatalf("code profile touching .env.local = %+v, want denied", g)
	}
	if g := open("mcp_tool_call", []string{"src/a.ts"}); g.Status != "pending" || g.DecisionID == nil {
		t.Fatalf("allowed path must still ask a human = %+v", g)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM approval_gate_event WHERE task_id = $1 AND details->>'reason' = 'permission_profile'`, task); n != 2 {
		t.Fatalf("denied gates = %d, want 2", n)
	}
}
