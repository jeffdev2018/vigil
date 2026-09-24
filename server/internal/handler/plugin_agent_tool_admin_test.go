package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The admin picker: GET lists every agent-trigger hook of an enabled
// installation with whether this agent is bound to it; PUT/DELETE change the
// binding. Mirrors the workspace-mcp-servers agent routes this was modeled on.

// agentToolHookTestManifest declares a hook with the agent trigger — unlike
// hookOnlyTestManifest's ui-only "summarize", which AvailableAgentPluginTools
// correctly excludes and so cannot be reused here.
const agentToolHookTestManifest = `{
  "manifest_version": 1,
  "key": "com.example.agenttool",
  "name": "Agent Tool",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read", "net:example.com"],
  "contributes": {
    "hooks": [{
      "key": "summarize",
      "name": "Summarize",
      "description": "Compress the discussion.",
      "triggers": ["agent"],
      "transport": { "type": "http", "url": "https://example.com/hooks/summarize" }
    }]
  }
}`

func TestAgentPluginToolsAdminEndpoints(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)

	runtimeID := dbfx.Runtime(t, "plugin-tools-admin runtime")
	agentID := dbfx.Agent(t, "plugin-tools-admin agent", runtimeID)

	installationID := dbfx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id":       testWorkspaceID,
		"plugin_key":         "com.example.plugin-tools-admin",
		"version":            "1.0.0",
		"manifest":           testutil.Raw(`'` + agentToolHookTestManifest + `'::jsonb`),
		"package_version_id": testutil.Raw("gen_random_uuid()"),
		"enabled":            true,
	})

	listReq := func() *http.Request {
		req := withURLParams(pluginHandlerRequest(http.MethodGet, "/api/agents/"+agentID+"/plugin-tools", nil, nil), "id", agentID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		return req
	}
	var list []agentPluginToolResponse

	// Before binding: the hook is available but not bound.
	testutil.Call(t, testHandler.ListAgentPluginTools, listReq()).Want(http.StatusOK).JSON(&list)
	if len(list) != 1 || list[0].HookKey != "summarize" {
		t.Fatalf("available tools = %+v, want exactly summarize", list)
	}
	if list[0].Bound {
		t.Fatal("summarize reported bound before anybody bound it")
	}

	// Bind.
	bindReq := withURLParams(pluginHandlerRequest(http.MethodPut,
		"/api/agents/"+agentID+"/plugin-tools/"+installationID+"/summarize", nil, nil),
		"id", agentID, "installationId", installationID, "hookKey", "summarize")
	bindReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	testutil.Call(t, testHandler.BindAgentPluginTool, bindReq).Want(http.StatusOK)

	testutil.Call(t, testHandler.ListAgentPluginTools, listReq()).Want(http.StatusOK).JSON(&list)
	if len(list) != 1 || !list[0].Bound {
		t.Fatalf("after bind, tools = %+v, want summarize bound", list)
	}

	// Binding again is idempotent (ON CONFLICT DO NOTHING), not a conflict.
	testutil.Call(t, testHandler.BindAgentPluginTool, bindReq).Want(http.StatusOK)

	// Unbind.
	unbindReq := withURLParams(pluginHandlerRequest(http.MethodDelete,
		"/api/agents/"+agentID+"/plugin-tools/"+installationID+"/summarize", nil, nil),
		"id", agentID, "installationId", installationID, "hookKey", "summarize")
	unbindReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	testutil.Call(t, testHandler.UnbindAgentPluginTool, unbindReq).Want(http.StatusNoContent)

	testutil.Call(t, testHandler.ListAgentPluginTools, listReq()).Want(http.StatusOK).JSON(&list)
	if len(list) != 1 || list[0].Bound {
		t.Fatalf("after unbind, tools = %+v, want summarize not bound", list)
	}

	// Unbinding something already unbound is a no-op, not an error.
	testutil.Call(t, testHandler.UnbindAgentPluginTool, unbindReq).Want(http.StatusNoContent)
}
