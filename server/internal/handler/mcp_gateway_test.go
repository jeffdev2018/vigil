package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/mcpgov"
)

// Governed MCP gateway (K77): a registered server gets a catalogue classified
// by risk; a binding carries a per-tool class capped by the trust dial and
// bounded by the Rule of Two; the claim carries the effective classes; every
// call the daemon reports lands in the run's audit trail, tracks usage and
// alerts the owner on high-risk tools; the monthly review proposes unused
// tools for removal. Pure classification, ceilings and the rule itself are
// tested in pkg/mcpgov.

// fakeMcpServer answers initialize and tools/list with four tools.
func fakeMcpServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"fake"}}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":{"tools":[
				{"name":"get_issue","description":"Read one issue","inputSchema":{"type":"object"}},
				{"name":"create_issue","description":"Create an issue","inputSchema":{"type":"object"}},
				{"name":"send_email","description":"Send an email to a customer","inputSchema":{"type":"object"}},
				{"name":"read_api_key","description":"Return the account api key","inputSchema":{"type":"object"}}]}}`))
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMcpGateway(t *testing.T) {
	ctx := context.Background()
	fake := fakeMcpServer(t)
	serverID := createWorkspaceMcpServerForTest(t, "crm-"+uuid.NewString()[:6], `{"url":"`+fake.URL+`"}`)
	ws := func(req *http.Request) *http.Request {
		return testutil.WithURLParams(req, "id", testWorkspaceID, "serverId", serverID)
	}

	// Discovery: four tools, classified by pattern; a manual risk survives a rediscovery.
	var cat struct {
		Tools []McpCatalogToolResponse `json:"tools"`
		Risks []string                 `json:"risks"`
	}
	testutil.Call(t, testHandler.DiscoverWorkspaceMcpServerTools, ws(newRequest(http.MethodPost, "/api/workspaces/x/mcp-servers/y/tools/discover", nil))).Want(http.StatusOK).JSON(&cat)
	risks := map[string]string{}
	for _, tool := range cat.Tools {
		risks[tool.Name] = tool.Risk
	}
	if len(cat.Tools) != 4 || risks["get_issue"] != mcpgov.RiskRead || risks["create_issue"] != mcpgov.RiskInternalWrite || risks["send_email"] != mcpgov.RiskExternal || risks["read_api_key"] != mcpgov.RiskSensitive || len(cat.Risks) != 5 {
		t.Fatalf("catalogue: %+v", cat)
	}
	testutil.Call(t, testHandler.SetWorkspaceMcpServerTools, ws(newRequest(http.MethodPut, "/api/workspaces/x/mcp-servers/y/tools", map[string]any{"tools": []map[string]any{{"name": "get_issue", "risk": "sensitive_data"}, {"name": "create_issue"}, {"name": "send_email"}, {"name": "read_api_key"}, {"name": "archive_all", "description": "hand-added"}}}))).Want(http.StatusOK).JSON(&cat)
	testutil.Call(t, testHandler.DiscoverWorkspaceMcpServerTools, ws(newRequest(http.MethodPost, "/api/workspaces/x/mcp-servers/y/tools/discover", nil))).Want(http.StatusOK).JSON(&cat)
	manual := false
	for _, tool := range cat.Tools {
		if tool.Name == "get_issue" && tool.Risk == "sensitive_data" && tool.RiskSource == "manual" {
			manual = true
		}
		if tool.Name == "archive_all" {
			t.Fatal("a tool the server does not expose is dropped by a rediscovery")
		}
	}
	if !manual {
		t.Fatalf("a manual risk survives rediscovery: %+v", cat.Tools)
	}
	testutil.Call(t, testHandler.SetWorkspaceMcpServerTools, ws(newRequest(http.MethodPut, "/api/workspaces/x/mcp-servers/y/tools", map[string]any{"tools": []map[string]any{{"name": "get_issue"}, {"name": "create_issue"}, {"name": "send_email"}, {"name": "read_api_key"}, {"name": "x", "risk": "bogus"}}}))).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.SetWorkspaceMcpServerTools, ws(newRequest(http.MethodPut, "/api/workspaces/x/mcp-servers/y/tools", map[string]any{"tools": []map[string]any{{"name": "get_issue", "risk": "read"}, {"name": "create_issue"}, {"name": "send_email"}, {"name": "read_api_key"}}}))).Want(http.StatusOK)
	stdio := createWorkspaceMcpServerForTest(t, "local-"+uuid.NewString()[:6], `{"command":"npx","args":["x"]}`)
	testutil.Call(t, testHandler.DiscoverWorkspaceMcpServerTools, testutil.WithURLParams(newRequest(http.MethodPost, "/x", nil), "id", testWorkspaceID, "serverId", stdio)).Want(http.StatusBadRequest)

	// Bindings: an approving agent may not run an external tool alone; the
	// Rule of Two refuses the set that reads, touches secrets and acts outside alone.
	agent := dbfx.Agent(t, "gateway agent "+uuid.NewString()[:6], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "approval"})
	agentReq := func(method, path string, body any) *http.Request {
		return testutil.WithURLParams(newRequest(method, path, body), "id", agent, "serverId", serverID)
	}
	var bound []WorkspaceMcpServerResponse
	testutil.Call(t, testHandler.AddAgentMcpServer, agentReq(http.MethodPost, "/api/agents/x/mcp-servers", map[string]any{"server_id": serverID})).Want(http.StatusOK).JSON(&bound)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_mcp_server WHERE agent_id = $1`, agent) })
	classes := map[string]string{}
	for _, tool := range bound[0].Tools {
		classes[tool.Name] = tool.Class
	}
	if bound[0].ToolCount != 4 || classes["get_issue"] != mcpgov.ClassActAlone || classes["create_issue"] != mcpgov.ClassActAlone || classes["send_email"] != mcpgov.ClassAsk || classes["read_api_key"] != mcpgov.ClassAsk {
		t.Fatalf("by-risk classes under an approval dial: %+v", classes)
	}
	res := testutil.Call(t, testHandler.SetAgentMcpServerPolicy, agentReq(http.MethodPut, "/x", map[string]any{"tools": map[string]string{"send_email": "act_alone"}})).Want(http.StatusBadRequest)
	if !strings.Contains(res.Body.String(), "trust dial") {
		t.Fatalf("ceiling refused: %s", res.Body.String())
	}
	dbfx.Exec(t, `UPDATE agent SET trust_mode = 'autonomous' WHERE id = $1`, agent)
	res = testutil.Call(t, testHandler.SetAgentMcpServerPolicy, agentReq(http.MethodPut, "/x", map[string]any{"tools": map[string]string{"send_email": "act_alone"}})).Want(http.StatusBadRequest)
	if !strings.Contains(res.Body.String(), "Rule of Two") {
		t.Fatalf("rule of two refused: %s", res.Body.String())
	}
	testutil.Call(t, testHandler.SetAgentMcpServerPolicy, agentReq(http.MethodPut, "/x", map[string]any{"tools": map[string]string{"send_email": "act_alone", "read_api_key": "never"}})).Want(http.StatusOK).JSON(&bound)
	classes = map[string]string{}
	for _, tool := range bound[0].Tools {
		classes[tool.Name] = tool.Class
	}
	if classes["send_email"] != mcpgov.ClassActAlone || classes["read_api_key"] != mcpgov.ClassNever || bound[0].ToolPolicy.Tools["read_api_key"] != "never" {
		t.Fatalf("policy saved: %+v", classes)
	}
	// A strict allowlist: only two tools are reachable.
	testutil.Call(t, testHandler.SetAgentMcpServerPolicy, agentReq(http.MethodPut, "/x", map[string]any{"default": "never", "tools": map[string]string{"get_issue": "act_alone", "send_email": "ask"}})).Want(http.StatusOK).JSON(&bound)

	// Claim: the gateway payload carries the effective classes.
	issue := dbfx.Issue(t, "gateway issue "+uuid.NewString()[:6], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "queued"})
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+handlerTestRuntimeID(t)+"/tasks/claim", nil, testWorkspaceID, "gateway-daemon")
	var claim struct {
		Task *struct {
			ID         string          `json:"id"`
			McpGateway *mcpgov.Gateway `json:"mcp_gateway"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, testutil.WithURLParams(req, "runtimeId", handlerTestRuntimeID(t))).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.McpGateway == nil || len(claim.Task.McpGateway.Servers) != 1 || claim.Task.McpGateway.TrustMode != "autonomous" {
		t.Fatalf("claim gateway: %+v", claim.Task)
	}
	gw := claim.Task.McpGateway.Servers[0]
	reachable := map[string]string{}
	for _, tool := range gw.Tools {
		if tool.Class != mcpgov.ClassNever {
			reachable[tool.Name] = tool.Class
		}
	}
	if gw.Default != "never" || gw.ServerID != serverID || len(reachable) != 2 || reachable["get_issue"] != mcpgov.ClassActAlone || reachable["send_email"] != mcpgov.ClassAsk {
		t.Fatalf("an agent bound to two tools sees only these two: %+v", gw)
	}
	taskID := claim.Task.ID
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	testHandler.recordRunSnapshot(ctx, mustTask(t, taskID), testWorkspaceID)
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.started' AND details->'mcp_bindings'->0->>'server' IS NOT NULL`, taskID) != 1 {
		t.Fatal("the replay snapshot records the bindings")
	}

	// Attribution: the daemon reports a call; audit, usage and the owner's alert follow.
	report := func(body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.ReportMcpCall, testutil.WithURLParams(runRequest(agent, taskID, http.MethodPost, "/api/tasks/x/mcp-calls", body), "taskId", taskID))
	}
	report(map[string]any{"server": gw.Name, "server_id": serverID, "tool": "send_email", "risk": "external_effect", "class": "ask", "result": "success", "gate_id": "g1", "duration_ms": 12, "first": true, "flags": []string{"secret_masked"}}).Want(http.StatusOK)
	report(map[string]any{"server": gw.Name, "server_id": serverID, "tool": "send_email", "risk": "external_effect", "class": "ask", "result": "success", "first": false}).Want(http.StatusOK)
	report(map[string]any{"server": gw.Name, "tool": "read_api_key", "risk": "sensitive_data", "class": "never", "result": "refused", "first": true}).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.mcp_tool_call'`, taskID) != 3 {
		t.Fatal("every call is an audit event of the run")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_mcp_server WHERE agent_id = $1 AND server_id = $2 AND tool_usage ? 'send_email'`, agent, serverID) != 1 {
		t.Fatal("a successful call stamps the tool's usage on the binding")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'mcp_alert' AND details->>'task_id' = $2`, testWorkspaceID, taskID) != 1 {
		t.Fatal("the owner is alerted once for the first successful high-risk call, approved or not")
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'mcp_alert'`, testWorkspaceID)
	})
	testutil.Call(t, testHandler.ReportMcpCall, testutil.WithURLParams(runRequest(agent, taskID, http.MethodPost, "/x", map[string]any{"tool": "x"}), "taskId", taskID)).Want(http.StatusBadRequest)
	// The daemon catalogues a server it brokered (how a stdio server is listed).
	testutil.Call(t, testHandler.ReportMcpCatalog, testutil.WithURLParams(runRequest(agent, taskID, http.MethodPost, "/x", map[string]any{"server": "local-unknown", "tools": []map[string]any{{"name": "a"}}}), "taskId", taskID)).Want(http.StatusOK)
	var stdioName string
	dbfx.QueryRow(t, `SELECT name FROM workspace_mcp_server WHERE id = $1`, stdio).Scan(&stdioName)
	testutil.Call(t, testHandler.ReportMcpCatalog, testutil.WithURLParams(runRequest(agent, taskID, http.MethodPost, "/x", map[string]any{"server": stdioName, "tools": []map[string]any{{"name": "run_script", "description": "runs a script", "schema_digest": "sha256:x"}, {"name": "list_files"}}}), "taskId", taskID)).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM workspace_mcp_server WHERE id = $1 AND jsonb_array_length(tools) = 2 AND tools->0->>'risk' = 'read'`, stdio) != 1 {
		t.Fatal("a daemon report catalogues the stdio server, classified")
	}

	// Review: a tool unused for thirty days on an old binding is proposed for removal.
	dbfx.Exec(t, `UPDATE agent_mcp_server SET created_at = now() - interval '40 days', tool_usage = jsonb_build_object('get_issue', now()) WHERE agent_id = $1`, agent)
	dbfx.Exec(t, `UPDATE agent_mcp_server SET tool_policy = '{}' WHERE agent_id = $1`, agent)
	acted, err := testHandler.ReviewMcpBindings(ctx, time.Now())
	if err != nil || acted < 1 {
		t.Fatalf("review: %d %v", acted, err)
	}
	var proposal string
	dbfx.QueryRow(t, `SELECT details->>'tools' FROM inbox_item WHERE workspace_id = $1 AND type = 'mcp_alert' AND details->>'kind' = 'mcp_binding_review' AND details->>'agent_id' = $2`, testWorkspaceID, agent).Scan(&proposal)
	if strings.Contains(proposal, "get_issue") || !strings.Contains(proposal, "send_email") {
		t.Fatalf("review proposes the unused tools only: %s", proposal)
	}

	// A plain member reads the catalogue; only an admin changes it.
	member := dbfx.User(t, "mcp member", "mcp-member-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	testutil.Call(t, testHandler.ListWorkspaceMcpServerTools, testutil.WithURLParams(newRequestAs(member, http.MethodGet, "/x", nil), "id", testWorkspaceID, "serverId", serverID)).Want(http.StatusOK)
	testutil.Call(t, testHandler.SetWorkspaceMcpServerTools, testutil.WithURLParams(newRequestAs(member, http.MethodPut, "/x", map[string]any{"tools": []map[string]any{}}), "id", testWorkspaceID, "serverId", serverID)).Want(http.StatusForbidden)
}
