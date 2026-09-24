package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// A disabled installation must lose every capability, not just the ones an
// admin remembered to revoke by hand. The MCP credential route is the one
// place the daemon's broker fetches a plugin's secret at dial time; if it kept
// answering after disable, disabling a plugin would look complete in the UI
// while its MCP server remained reachable with a live credential.
func TestResolvePluginMCPCredentialRefusesDisabledInstallation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "mcp-disabled-cred runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "mcp-disabled-cred agent")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":    runtimeID,
		"issue_id":      issueID,
		"status":        "dispatched",
		"dispatched_at": testutil.Raw("now()"),
	})

	installationID := dbfx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id":       testWorkspaceID,
		"plugin_key":         "com.example.mcp-disabled-cred",
		"version":            "1.0.0",
		"manifest":           testutil.Raw(`'` + mcpToolboxManifest + `'::jsonb`),
		"granted_scopes":     testutil.Raw(`'["issues:read","net:tools.example.com"]'::jsonb`),
		"package_version_id": testutil.Raw("gen_random_uuid()"),
		"enabled":            false,
	})
	contributionID := remotemcp.PluginContributionPrefix + installationID + ":toolbox"

	req := newDaemonTokenRequest(http.MethodGet,
		"/api/daemon/tasks/"+taskID+"/plugin-mcp/"+contributionID+"/credential", nil, testWorkspaceID, "mcp-disabled-cred-daemon")
	req = withURLParams(req, "id", taskID, "contributionId", contributionID)
	testutil.Call(t, testHandler.ResolvePluginMCPCredential, req).Want(http.StatusForbidden)

	dbfx.Exec(t, "UPDATE plugin_installation SET enabled = TRUE WHERE id = $1", installationID)

	req = newDaemonTokenRequest(http.MethodGet,
		"/api/daemon/tasks/"+taskID+"/plugin-mcp/"+contributionID+"/credential", nil, testWorkspaceID, "mcp-disabled-cred-daemon")
	req = withURLParams(req, "id", taskID, "contributionId", contributionID)
	testutil.Call(t, testHandler.ResolvePluginMCPCredential, req).Want(http.StatusOK)
}
