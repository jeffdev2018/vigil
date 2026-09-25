package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// Deny by default: an installed, enabled plugin's tools used to be offered to
// every agent in the workspace just because the workspace had the plugin
// installed. These pin that AgentHookTools, AgentMCPConnections and
// AvailableAgentPluginTools all now require an explicit agent_plugin_tool
// binding, and that Uninstall removes it with everything else the
// installation owned.

const bindingHookManifest = `{
	"manifest_version": 1,
	"key": "com.example.binding",
	"name": "Binding",
	"description": "d",
	"version": "1.0.0",
	"author": {"name": "example"},
	"scopes": ["issues:read", "net:hooks.example.com", "net:tools.example.com"],
	"contributes": {"hooks": [
		{"key": "http_tool", "name": "HTTP tool", "description": "An http agent tool.",
		 "triggers": ["agent"], "transport": {"type": "http", "url": "https://hooks.example.com/tool"}},
		{"key": "mcp_tool", "name": "MCP tool", "description": "An mcp agent tool.",
		 "triggers": ["agent"], "transport": {"type": "mcp", "url": "https://tools.example.com/mcp"}}
	]}
}`

func setupAgentBindingFixture(t *testing.T) (svc *PluginService, workspaceID string, agentID string, installation db.PluginInstallation) {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("bind-owner-%d", suffix), fmt.Sprintf("bind-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("bind-ws-%d", suffix), fmt.Sprintf("bind-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, fmt.Sprintf("bind-runtime-%d", suffix))
	agent := fx.Agent(t, fmt.Sprintf("bind-agent-%d", suffix), runtimeID)

	installationID := fx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id":       ws,
		"plugin_key":         fmt.Sprintf("com.example.binding-%d", suffix),
		"version":            "1.0.0",
		"manifest":           testutil.Raw(`'` + bindingHookManifest + `'::jsonb`),
		"granted_scopes":     testutil.Raw(`'["issues:read","net:hooks.example.com","net:tools.example.com"]'::jsonb`),
		"package_version_id": testutil.Raw("gen_random_uuid()"),
		"enabled":            true,
	})

	// The mcp hook also needs a matching, endpoint-current approval or
	// mcpConnectionsFor refuses it before the binding gate is ever reached.
	approvals, err := json.Marshal(PluginMCPApprovals{
		"mcp_tool": {Tools: []remotemcp.Tool{{Name: "search", SchemaDigest: "sha256:aaa"}}, Endpoint: "https://tools.example.com/mcp"},
	})
	if err != nil {
		t.Fatalf("marshal approvals: %v", err)
	}
	// ApproveMCPHookTools would re-discover against a live server; write the
	// approval directly since this test is about the binding gate, not
	// discovery.
	fx.Exec(t, "UPDATE plugin_installation SET mcp_approvals = $1 WHERE id = $2", approvals, installationID)

	queries := db.New(pool)
	svc = &PluginService{Queries: queries, TxStarter: pool}
	loaded, err := queries.GetWorkspacePluginInstallation(context.Background(), db.GetWorkspacePluginInstallationParams{
		WorkspaceID: util.MustParseUUID(ws), ID: util.MustParseUUID(installationID),
	})
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	return svc, ws, agent, loaded
}

func TestAgentHookToolsDenyByDefaultUntilBound(t *testing.T) {
	svc, ws, agentID, installation := setupAgentBindingFixture(t)
	ctx := context.Background()

	tools, err := svc.AgentHookTools(ctx, util.MustParseUUID(ws), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("AgentHookTools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("an unbound agent got %d hook tools, want 0 (deny by default)", len(tools))
	}

	if _, err := svc.Queries.BindAgentPluginTool(ctx, db.BindAgentPluginToolParams{
		WorkspaceID: util.MustParseUUID(ws), AgentID: util.MustParseUUID(agentID),
		InstallationID: installation.ID, HookKey: "http_tool",
	}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	tools, err = svc.AgentHookTools(ctx, util.MustParseUUID(ws), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("AgentHookTools after bind: %v", err)
	}
	if len(tools) != 1 || tools[0].HookKey != "http_tool" {
		t.Fatalf("bound tools = %+v, want exactly http_tool", tools)
	}
}

func TestAgentMCPConnectionsDenyByDefaultUntilBound(t *testing.T) {
	svc, ws, agentID, installation := setupAgentBindingFixture(t)
	ctx := context.Background()

	connections, err := svc.AgentMCPConnections(ctx, util.MustParseUUID(ws), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("AgentMCPConnections: %v", err)
	}
	if len(connections) != 0 {
		t.Fatalf("an unbound agent got %d mcp connections, want 0 (deny by default)", len(connections))
	}

	if _, err := svc.Queries.BindAgentPluginTool(ctx, db.BindAgentPluginToolParams{
		WorkspaceID: util.MustParseUUID(ws), AgentID: util.MustParseUUID(agentID),
		InstallationID: installation.ID, HookKey: "mcp_tool",
	}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	connections, err = svc.AgentMCPConnections(ctx, util.MustParseUUID(ws), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("AgentMCPConnections after bind: %v", err)
	}
	if len(connections) != 1 {
		t.Fatalf("got %d connections after bind, want 1", len(connections))
	}
}

func TestAvailableAgentPluginToolsReportsBoundFlag(t *testing.T) {
	svc, ws, agentID, installation := setupAgentBindingFixture(t)
	ctx := context.Background()

	available, err := svc.AvailableAgentPluginTools(ctx, util.MustParseUUID(ws), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("AvailableAgentPluginTools: %v", err)
	}
	if len(available) != 2 {
		t.Fatalf("available tools = %+v, want both declared hooks listed regardless of binding", available)
	}
	for _, tool := range available {
		if tool.Bound {
			t.Fatalf("tool %q reported bound before anybody bound it", tool.HookKey)
		}
	}

	if _, err := svc.Queries.BindAgentPluginTool(ctx, db.BindAgentPluginToolParams{
		WorkspaceID: util.MustParseUUID(ws), AgentID: util.MustParseUUID(agentID),
		InstallationID: installation.ID, HookKey: "http_tool",
	}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	available, err = svc.AvailableAgentPluginTools(ctx, util.MustParseUUID(ws), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("AvailableAgentPluginTools after bind: %v", err)
	}
	boundCount := 0
	for _, tool := range available {
		if tool.Bound {
			boundCount++
			if tool.HookKey != "http_tool" {
				t.Fatalf("wrong tool reported bound: %q", tool.HookKey)
			}
		}
	}
	if boundCount != 1 {
		t.Fatalf("bound tool count = %d, want 1", boundCount)
	}
}

func TestUninstallRemovesAgentPluginToolBindings(t *testing.T) {
	svc, ws, agentID, installation := setupAgentBindingFixture(t)
	ctx := context.Background()

	if _, err := svc.Queries.BindAgentPluginTool(ctx, db.BindAgentPluginToolParams{
		WorkspaceID: util.MustParseUUID(ws), AgentID: util.MustParseUUID(agentID),
		InstallationID: installation.ID, HookKey: "http_tool",
	}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	if err := svc.Uninstall(ctx, installation); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	bindings, err := svc.Queries.ListAgentPluginTools(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("list bindings after uninstall: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("bindings survived uninstall: %+v", bindings)
	}
}
