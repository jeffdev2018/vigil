package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// The MCP-transport tools/call bypasses the server entirely — the broker
// dials the plugin's own MCP server directly — so this route is the only
// place the rate limit, circuit breaker and invocation log an http hook gets
// through InvokeHook also apply to an mcp hook. These pin the two request
// shapes the daemon sends: the gate call (no body) and the report call
// (`result` set), and that a disabled installation is refused like every
// other plugin capability.

func mcpCallsRequest(taskID, contributionID string, body any) *http.Request {
	req := newDaemonTokenRequest(http.MethodPost,
		"/api/daemon/tasks/"+taskID+"/plugin-mcp/"+contributionID+"/calls", body, testWorkspaceID, "mcp-calls-daemon")
	return withURLParams(req, "id", taskID, "contributionId", contributionID)
}

func TestRecordPluginMCPCallGateAndReport(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "mcp-calls runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "mcp-calls agent")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":    runtimeID,
		"issue_id":      issueID,
		"status":        "dispatched",
		"dispatched_at": testutil.Raw("now()"),
	})

	installationID := dbfx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id":       testWorkspaceID,
		"plugin_key":         "com.example.mcp-calls",
		"version":            "1.0.0",
		"manifest":           testutil.Raw(`'` + mcpToolboxManifest + `'::jsonb`),
		"granted_scopes":     testutil.Raw(`'["issues:read","net:tools.example.com"]'::jsonb`),
		"package_version_id": testutil.Raw("gen_random_uuid()"),
		"enabled":            true,
	})
	contributionID := remotemcp.PluginContributionPrefix + installationID + ":toolbox"

	// Gate call: no body, must admit the call.
	var gateResp struct {
		Allowed bool `json:"allowed"`
	}
	testutil.Call(t, testHandler.RecordPluginMCPCall, mcpCallsRequest(taskID, contributionID, map[string]any{})).
		Want(http.StatusOK).JSON(&gateResp)
	if !gateResp.Allowed {
		t.Fatal("gate call did not allow an enabled installation's call")
	}

	// Report call: result set, must record the outcome and answer ok.
	testutil.Call(t, testHandler.RecordPluginMCPCall, mcpCallsRequest(taskID, contributionID, map[string]any{
		"result": "failed", "latency_ms": 123, "error": "upstream exploded",
	})).Want(http.StatusOK)

	// Disable the installation: every capability, including this one, must
	// refuse afterward.
	dbfx.Exec(t, "UPDATE plugin_installation SET enabled = FALSE WHERE id = $1", installationID)
	testutil.Call(t, testHandler.RecordPluginMCPCall, mcpCallsRequest(taskID, contributionID, map[string]any{})).
		Want(http.StatusForbidden)
}

func TestRecordPluginMCPCallRejectsMalformedContributionID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "mcp-calls-bad runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "mcp-calls-bad agent")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":    runtimeID,
		"issue_id":      issueID,
		"status":        "dispatched",
		"dispatched_at": testutil.Raw("now()"),
	})

	testutil.Call(t, testHandler.RecordPluginMCPCall, mcpCallsRequest(taskID, "not-a-plugin-contribution", map[string]any{})).
		Want(http.StatusBadRequest)
}
