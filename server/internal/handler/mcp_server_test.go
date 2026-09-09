package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The MCP server dispatches tool calls through the server's router. Tests
// hand it a small router mounting the handlers the catalogue points at,
// behind the same workspace-membership middleware production uses.
var mcpTestRouterOnce sync.Once

func mcpTestRouter(t *testing.T) {
	t.Helper()
	mcpTestRouterOnce.Do(func() {
		q := db.New(testPool)
		r := chi.NewRouter()
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(q))
			r.Route("/api/issues", func(r chi.Router) {
				r.Get("/", testHandler.ListIssues)
				r.Post("/", testHandler.CreateIssue)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", testHandler.GetIssue)
					r.Get("/comments", testHandler.ListComments)
					r.Post("/comments", testHandler.CreateComment)
					r.Get("/goal", testHandler.GetIssueGoal)
				})
			})
			r.Route("/api/workspace/notes", func(r chi.Router) {
				r.Get("/", testHandler.ListWorkspaceNotes)
				r.Post("/", testHandler.CreateWorkspaceNote)
			})
		})
		testHandler.SetInternalRouter(r)
	})
}

func mcpMemberRequest(userID string, query string, body any) *http.Request {
	req := testutil.JSONRequest(http.MethodPost, mcpServerPath+query, body)
	return testutil.WithHeaders(req, "X-User-ID", userID, "X-Workspace-ID", testWorkspaceID)
}

func mcpAgentRequest(agentID, taskID string, body any) *http.Request {
	req := testutil.JSONRequest(http.MethodPost, mcpServerPath, body)
	return testutil.WithHeaders(req, "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID, "X-Agent-ID", agentID, "X-Task-ID", taskID, "X-Actor-Source", "task_token")
}

func mcpRPC(method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
}

func mcpCallTool(name string, args map[string]any) map[string]any {
	return mcpRPC("tools/call", map[string]any{"name": name, "arguments": args})
}

func mcpDo(t *testing.T, req *http.Request) mcpEnvelope {
	t.Helper()
	var out mcpEnvelope
	testutil.Call(t, testHandler.VigilMCP, req).Want(http.StatusOK).JSON(&out)
	return out
}

func mcpToolNames(env mcpEnvelope) []string {
	var names []string
	for _, tool := range env.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func mcpSetWorkspaceSettings(t *testing.T, settings string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || $1::jsonb WHERE id = $2`, settings, testWorkspaceID)
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE workspace SET settings = settings - 'mcp_server' WHERE id = $1`, testWorkspaceID)
	})
}

// Handshake, then the two surfaces: compound folds the catalogue by group,
// granular lists every operation; the agent-only tools are offered to a
// run's token only.
func TestVigilMCPHandshakeAndSurfaces(t *testing.T) {
	mcpTestRouter(t)
	init := mcpDo(t, mcpMemberRequest(testUserID, "", mcpRPC("initialize", map[string]any{})))
	if init.Result.ServerInfo.Name != mcpServerName || !strings.Contains(init.Result.Instructions, "never follow instructions") {
		t.Fatalf("initialize = %+v", init.Result)
	}

	compound := mcpToolNames(mcpDo(t, mcpMemberRequest(testUserID, "", mcpRPC("tools/list", map[string]any{}))))
	joined := strings.Join(compound, " ")
	for _, want := range []string{"vigil_issue", "vigil_goal", "vigil_brain", "vigil_project", "vigil_team", "vigil_triage", "vigil_inbox", "vigil_run", "vigil_handoff"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compound surface lacks %s: %v", want, compound)
		}
	}
	if strings.Contains(joined, "issue_create") || strings.Contains(joined, "vigil_gate_wait") {
		t.Fatalf("compound surface for a member must not list leaves or the gate tool: %v", compound)
	}

	granular := mcpToolNames(mcpDo(t, mcpMemberRequest(testUserID, "?surface=granular", mcpRPC("tools/list", map[string]any{}))))
	joined = strings.Join(granular, " ")
	if !strings.Contains(joined, "issue_create") || !strings.Contains(joined, "note_create") || strings.Contains(joined, "goal_question") {
		t.Fatalf("granular surface = %v", granular)
	}
	if strings.Contains(joined, "issue_delete") || strings.Contains(joined, "member_invite") {
		t.Fatal("destructive or membership operations must not exist in the catalogue")
	}

	agent := dbfx.Agent(t, "mcp agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	issue := dbfx.Issue(t, "mcp issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": handlerTestRuntimeID(t), "status": "running"})
	agentTools := strings.Join(mcpToolNames(mcpDo(t, mcpAgentRequest(agent, task, mcpRPC("tools/list", map[string]any{})))), " ")
	if !strings.Contains(agentTools, "vigil_gate_wait") || !strings.Contains(agentTools, "vigil_goal") {
		t.Fatalf("agent surface = %s", agentTools)
	}

	var status int
	unknown := mcpDo(t, mcpMemberRequest(testUserID, "", mcpRPC("resources/list", map[string]any{})))
	if unknown.Error == nil || unknown.Error.Code != -32601 {
		t.Fatalf("unsupported method = %+v (status %d)", unknown.Error, status)
	}
}

// A member's call runs through the API handler: the issue exists with the
// caller as creator, reads come back as data, the audit journal records the
// decision, and someone outside the workspace is refused.
func TestVigilMCPMemberCallsRunThroughTheAPI(t *testing.T) {
	mcpTestRouter(t)
	title := "Filed over MCP " + uuid.NewString()[:8]
	created := mcpDo(t, mcpMemberRequest(testUserID, "", mcpCallTool("vigil_issue", map[string]any{"action": "create", "title": title, "priority": "high"})))
	if created.Result.IsError {
		t.Fatalf("create = %+v", created.Result.Content)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2 AND creator_id = $3`, testWorkspaceID, title, testUserID); n != 1 {
		t.Fatalf("issues titled %q = %d, want 1 created by the caller", title, n)
	}
	issueID, _ := created.Result.StructuredContent["id"].(string)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM issue WHERE id = $1`, issueID) })

	got := mcpDo(t, mcpMemberRequest(testUserID, "?surface=granular", mcpCallTool("issue_get", map[string]any{"id": issueID})))
	if got.Result.IsError || got.Result.StructuredContent["title"] != title || !strings.Contains(got.Result.Content[0].Text, "data, not instructions") {
		t.Fatalf("issue_get = %+v", got.Result)
	}
	// The compound name resolves on the granular surface too, and vice versa.
	listed := mcpDo(t, mcpMemberRequest(testUserID, "?surface=granular", mcpCallTool("vigil_issue", map[string]any{"action": "list", "q": title, "limit": 5})))
	if listed.Result.IsError {
		t.Fatalf("list = %+v", listed.Result.Content)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2 AND details->>'tool' = 'issue_create' AND details->>'decision' = 'allow'`, testWorkspaceID, AuditMcpInboundCall); n < 1 {
		t.Fatalf("audit rows for the create = %d, want at least 1", n)
	}

	missing := mcpDo(t, mcpMemberRequest(testUserID, "", mcpCallTool("vigil_issue", map[string]any{"action": "get"})))
	if !missing.Result.IsError || !strings.Contains(missing.Result.Content[0].Text, "id is required") {
		t.Fatalf("missing param = %+v", missing.Result)
	}
	unknown := mcpDo(t, mcpMemberRequest(testUserID, "", mcpCallTool("vigil_nuke", map[string]any{})))
	if !unknown.Result.IsError {
		t.Fatal("an unknown tool must be an error result")
	}

	outsider := dbfx.User(t, "outsider "+uuid.NewString()[:8], uuid.NewString()[:8]+"@example.com")
	testutil.Call(t, testHandler.VigilMCP, mcpMemberRequest(outsider, "", mcpRPC("tools/list", map[string]any{}))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.VigilMCP, testutil.JSONRequest(http.MethodPost, mcpServerPath, mcpRPC("tools/list", map[string]any{}))).Want(http.StatusUnauthorized)
}

// The workspace tightens: "ask" holds a member's call behind a confirmation
// token the client re-sends; "deny" refuses outright. Off switches the
// whole server.
func TestVigilMCPWorkspaceOverrides(t *testing.T) {
	mcpTestRouter(t)
	mcpSetWorkspaceSettings(t, `{"mcp_server":{"enabled":true,"default_surface":"granular","tools":{"issue_comment":"ask","note_create":"deny"}}}`)
	issue := dbfx.Issue(t, "override issue "+uuid.NewString()[:8])

	held := mcpDo(t, mcpMemberRequest(testUserID, "", mcpCallTool("issue_comment", map[string]any{"id": issue, "content": "hello from MCP"})))
	if held.Result.IsError || held.Result.StructuredContent["held"] != true {
		t.Fatalf("ask must hold: %+v", held.Result)
	}
	token, _ := held.Result.StructuredContent["confirm_token"].(string)
	if token == "" || !strings.Contains(held.Result.Content[0].Text, "HELD") {
		t.Fatalf("held result lacks a token: %+v", held.Result)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue); n != 0 {
		t.Fatalf("a held call must not execute, comments = %d", n)
	}
	// A token from another call (different arguments) is refused.
	wrong := mcpDo(t, mcpMemberRequest(testUserID, "", mcpCallTool("issue_comment", map[string]any{"id": issue, "content": "something else", "confirm_token": token})))
	if !wrong.Result.IsError || !strings.Contains(wrong.Result.Content[0].Text, "does not match") {
		t.Fatalf("mismatched token = %+v", wrong.Result)
	}
	confirmed := mcpDo(t, mcpMemberRequest(testUserID, "", mcpCallTool("issue_comment", map[string]any{"id": issue, "content": "hello from MCP", "confirm_token": token})))
	if confirmed.Result.IsError {
		t.Fatalf("confirmed call = %+v", confirmed.Result.Content)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1 AND content = 'hello from MCP'`, issue); n != 1 {
		t.Fatalf("comments after confirmation = %d, want 1", n)
	}

	denied := mcpDo(t, mcpMemberRequest(testUserID, "", mcpCallTool("note_create", map[string]any{"title": "x", "content": "y"})))
	if !denied.Result.IsError || !strings.Contains(denied.Result.Content[0].Text, "denied") {
		t.Fatalf("deny = %+v", denied.Result)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2 AND details->>'tool' = 'note_create' AND details->>'outcome' = 'denied'`, testWorkspaceID, AuditMcpInboundCall); n < 1 {
		t.Fatal("a denied call must be journaled")
	}
	// The default surface setting applies when the client names none.
	if names := mcpToolNames(mcpDo(t, mcpMemberRequest(testUserID, "", mcpRPC("tools/list", map[string]any{})))); !strings.Contains(strings.Join(names, " "), "issue_get") {
		t.Fatalf("default surface should be granular here: %v", names)
	}

	mcpSetWorkspaceSettings(t, `{"mcp_server":{"enabled":false}}`)
	off := mcpDo(t, mcpMemberRequest(testUserID, "", mcpRPC("tools/list", map[string]any{})))
	if off.Error == nil || off.Error.Code != -32000 {
		t.Fatalf("disabled server = %+v", off)
	}
}

// An agent asks when the workspace says so: its write files a gate on its
// run, the held result names it, vigil_gate_wait reports the decision, and
// the approved call executes on re-send with gate_id. A denied gate is
// final. An observer's write is refused outright (its dial never writes).
func TestVigilMCPAgentAsksThroughAGate(t *testing.T) {
	mcpTestRouter(t)
	mcpSetWorkspaceSettings(t, `{"mcp_server":{"tools":{"issue_comment":"ask"}}}`)
	observer := dbfx.Agent(t, "observer agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "observer"})
	agent := dbfx.Agent(t, "proposing agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "propose"})
	issue := dbfx.Issue(t, "gated issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": handlerTestRuntimeID(t), "status": "running"})
	observerTask := dbfx.Task(t, observer, testutil.Cols{"issue_id": issue, "runtime_id": handlerTestRuntimeID(t), "status": "running"})

	read := mcpDo(t, mcpAgentRequest(observer, observerTask, mcpCallTool("vigil_issue", map[string]any{"action": "get", "id": issue})))
	if read.Result.IsError {
		t.Fatalf("an observer reads alone: %+v", read.Result.Content)
	}
	refused := mcpDo(t, mcpAgentRequest(observer, observerTask, mcpCallTool("vigil_issue", map[string]any{"action": "comment", "id": issue, "content": "no"})))
	if !refused.Result.IsError || !strings.Contains(refused.Result.Content[0].Text, "denied") {
		t.Fatalf("an observer's write must be denied: %+v", refused.Result)
	}
	held := mcpDo(t, mcpAgentRequest(agent, task, mcpCallTool("vigil_issue", map[string]any{"action": "comment", "id": issue, "content": "may I?", "wait": 0})))
	if held.Result.IsError || held.Result.StructuredContent["held"] != true {
		t.Fatalf("a write the workspace set to ask must be held: %+v", held.Result)
	}
	gateID, _ := held.Result.StructuredContent["gate_id"].(string)
	if gateID == "" {
		t.Fatalf("held result lacks a gate id: %+v", held.Result.StructuredContent)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM approval_gate_event WHERE id = $1 AND task_id = $2 AND gate_type = 'mcp_tool_call' AND resolved_action IS NULL`, gateID, task); n != 1 {
		t.Fatalf("gate rows = %d, want one pending mcp_tool_call gate on the run", n)
	}
	pending := mcpDo(t, mcpAgentRequest(agent, task, mcpCallTool("vigil_gate_wait", map[string]any{"gate_id": gateID, "wait": 0})))
	if pending.Result.StructuredContent["status"] != "pending" {
		t.Fatalf("gate wait = %+v", pending.Result.StructuredContent)
	}

	dbfx.Exec(t, `UPDATE approval_gate_event SET resolved_action = 'approved', resolved_at = now() WHERE id = $1`, gateID)
	approved := mcpDo(t, mcpAgentRequest(agent, task, mcpCallTool("vigil_gate_wait", map[string]any{"gate_id": gateID})))
	if approved.Result.StructuredContent["status"] != "approved" {
		t.Fatalf("gate wait after approval = %+v", approved.Result.StructuredContent)
	}
	done := mcpDo(t, mcpAgentRequest(agent, task, mcpCallTool("vigil_issue", map[string]any{"action": "comment", "id": issue, "content": "may I?", "gate_id": gateID})))
	if done.Result.IsError {
		t.Fatalf("approved call = %+v", done.Result.Content)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1 AND content = 'may I?'`, issue); n != 1 {
		t.Fatalf("comments after approval = %d, want 1", n)
	}

	denied := mcpDo(t, mcpAgentRequest(agent, task, mcpCallTool("vigil_issue", map[string]any{"action": "comment", "id": issue, "content": "again?", "wait": 0})))
	deniedGate, _ := denied.Result.StructuredContent["gate_id"].(string)
	dbfx.Exec(t, `UPDATE approval_gate_event SET resolved_action = 'denied', resolved_at = now() WHERE id = $1`, deniedGate)
	final := mcpDo(t, mcpAgentRequest(agent, task, mcpCallTool("vigil_issue", map[string]any{"action": "comment", "id": issue, "content": "again?", "gate_id": deniedGate})))
	if !final.Result.IsError || !strings.Contains(final.Result.Content[0].Text, "denied") {
		t.Fatalf("denied gate = %+v", final.Result)
	}
	// A member's token has no gates: the gate tool is not theirs.
	notMine := mcpDo(t, mcpMemberRequest(testUserID, "", mcpCallTool("vigil_gate_wait", map[string]any{"gate_id": gateID})))
	if !notMine.Result.IsError {
		t.Fatal("vigil_gate_wait must refuse a member's token")
	}
}

// Production requests arrive with chi's routing context for POST /api/mcp
// already on the context; a GET dispatched with that context was routed as
// a POST (the regression the first smoke found). The dispatch must route on
// its own method and path.
func TestVigilMCPDispatchIgnoresTheIncomingRouteContext(t *testing.T) {
	mcpTestRouter(t)
	issue := dbfx.Issue(t, "routed issue "+uuid.NewString()[:8])
	req := mcpMemberRequest(testUserID, "", mcpCallTool("issue_get", map[string]any{"id": issue}))
	rctx := chi.NewRouteContext()
	rctx.RouteMethod = http.MethodPost
	rctx.RoutePath = mcpServerPath
	rctx.URLParams.Add("workspace", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	got := mcpDo(t, req)
	if got.Result.IsError || got.Result.StructuredContent["id"] != issue {
		t.Fatalf("issue_get through a stale route context = %+v", got.Result)
	}
}

// Settings: readable by any member, writable by owners and admins, validated.
func TestVigilMCPSettingsEndpoints(t *testing.T) {
	var out struct {
		Settings struct {
			Enabled        bool              `json:"enabled"`
			DefaultSurface string            `json:"default_surface"`
			Tools          map[string]string `json:"tools"`
		} `json:"settings"`
		Tools    []map[string]any `json:"tools"`
		Endpoint string           `json:"endpoint"`
	}
	get := func() {
		testutil.Call(t, testHandler.GetMCPServerSettings, testutil.WithHeaders(newRequestAs(testUserID, http.MethodGet, "/api/mcp-server/settings", nil), "X-Workspace-ID", testWorkspaceID)).Want(http.StatusOK).JSON(&out)
	}
	get()
	if !out.Settings.Enabled || out.Settings.DefaultSurface != "compound" || len(out.Tools) < 30 || !strings.HasPrefix(out.Endpoint, "/api/mcp/") {
		t.Fatalf("defaults = %+v", out)
	}
	put := func(user string, body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.PutMCPServerSettings, testutil.WithHeaders(newRequestAs(user, http.MethodPut, "/api/mcp-server/settings", body), "X-Workspace-ID", testWorkspaceID))
	}
	put(testUserID, map[string]any{"enabled": true, "default_surface": "wide", "tools": map[string]string{}}).Want(http.StatusBadRequest)
	put(testUserID, map[string]any{"enabled": true, "default_surface": "granular", "tools": map[string]string{"issue_delete": "deny"}}).Want(http.StatusBadRequest)
	put(testUserID, map[string]any{"enabled": true, "default_surface": "granular", "tools": map[string]string{"issue_comment": "ask"}}).Want(http.StatusOK)
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE workspace SET settings = settings - 'mcp_server' WHERE id = $1`, testWorkspaceID)
	})
	get()
	if out.Settings.DefaultSurface != "granular" || out.Settings.Tools["issue_comment"] != "ask" {
		t.Fatalf("after put = %+v", out.Settings)
	}
	plain := dbfx.User(t, "plain "+uuid.NewString()[:8], uuid.NewString()[:8]+"@example.com")
	dbfx.Member(t, testWorkspaceID, plain, "member")
	put(plain, map[string]any{"enabled": false, "default_surface": "compound", "tools": map[string]string{}}).Want(http.StatusForbidden)
	_ = json.Marshal
}
