package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Deny by default: an agent-tool hook the manifest declares is not reachable
// by an agent nobody bound it to, even though the installation is enabled and
// the hook exists. InvokeAgentPluginHook answers with a hard 403 for this —
// distinct from every hook-execution failure below it, which the daemon reads
// as a tool error instead.

func invokeAgentHookRequestFor(taskID string, body invokeAgentHookRequest) *http.Request {
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/plugin-hooks", body, testWorkspaceID, "agent-hook-binding-daemon")
	return withURLParams(req, "id", taskID)
}

func TestInvokeAgentPluginHookRefusesUnboundHook(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "agent-hook-binding runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "agent-hook-binding agent")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":    runtimeID,
		"issue_id":      issueID,
		"status":        "dispatched",
		"dispatched_at": testutil.Raw("now()"),
	})

	installationID := dbfx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id":       testWorkspaceID,
		"plugin_key":         "com.example.agent-hook-binding",
		"version":            "1.0.0",
		"manifest":           testutil.Raw(`'` + hookOnlyTestManifest + `'::jsonb`),
		"package_version_id": testutil.Raw("gen_random_uuid()"),
		"enabled":            true,
	})

	// Never bound: the manifest declares "summarize" (even with a ui trigger
	// here, not agent — irrelevant, the bound-check runs before any manifest
	// read at all) but nobody granted this agent access to it.
	testutil.Call(t, testHandler.InvokeAgentPluginHook, invokeAgentHookRequestFor(taskID, invokeAgentHookRequest{
		InstallationID: installationID, HookKey: "summarize",
	})).Want(http.StatusForbidden)

	// Bound: the gate passes, and the request proceeds into InvokeAgentHook's
	// own territory (which reports failure as 200 + error body, not a 4xx —
	// tested elsewhere). Reaching that different failure mode proves the 403
	// above was specifically the binding gate, not a blanket refusal.
	if _, err := testHandler.Queries.BindAgentPluginTool(ctx, db.BindAgentPluginToolParams{
		WorkspaceID: parseUUID(testWorkspaceID), AgentID: parseUUID(agentID),
		InstallationID: parseUUID(installationID), HookKey: "summarize",
	}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	resp := testutil.Call(t, testHandler.InvokeAgentPluginHook, invokeAgentHookRequestFor(taskID, invokeAgentHookRequest{
		InstallationID: installationID, HookKey: "summarize",
	}))
	if resp.Code == http.StatusForbidden {
		t.Fatalf("a bound hook was still refused with 403: %s", resp.Body.String())
	}
}
