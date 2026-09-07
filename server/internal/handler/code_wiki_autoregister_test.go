package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// F26 acceptance 6: the first publication registers the hosted MCP server for
// the workspace and binds it to the generating agent; every later publication
// changes nothing.

func TestCodeWikiMcpAutoRegistrationIsIdempotent(t *testing.T) {
	agentID := dbfx.Agent(t, "wiki publisher "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	dbfx.Cleanup(t, `DELETE FROM agent_mcp_server WHERE agent_id = $1`, agentID)
	dbfx.Cleanup(t, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, codeWikiMCPServerName)
	dbfx.Exec(t, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, codeWikiMCPServerName)

	publish := func(repo string) {
		t.Helper()
		projectID, _ := wikiFixture(t, "https://github.com/acme/"+repo)
		snapshot := startWikiSnapshot(t, projectID, []string{"src/app.py"})
		testutil.Call(t, testHandler.CreateProjectCodeWikiPage,
			testutil.WithHeaders(
				wikiRequest(t, http.MethodPost, "/x", map[string]any{
					"slug": "overview", "title": "Overview", "content": "body",
					"citations": []map[string]any{{"path": "src/app.py"}},
				}, "id", projectID, "sid", snapshot.ID),
				"X-Agent-ID", agentID, "X-Actor-Source", "task_token"),
		).Want(http.StatusCreated)
		testutil.Call(t, testHandler.PublishProjectCodeWikiSnapshot,
			testutil.WithHeaders(
				wikiRequest(t, http.MethodPost, "/x", nil, "id", projectID, "sid", snapshot.ID),
				"X-Agent-ID", agentID, "X-Actor-Source", "task_token"),
		).Want(http.StatusOK)
	}

	countRows := func() (servers, bindings int) {
		t.Helper()
		dbfx.QueryRow(t,
			`SELECT count(*) FROM workspace_mcp_server WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, codeWikiMCPServerName).Scan(&servers)
		dbfx.QueryRow(t,
			`SELECT count(*) FROM agent_mcp_server ams
			 JOIN workspace_mcp_server s ON s.id = ams.server_id
			 WHERE ams.agent_id = $1 AND s.name = $2`,
			agentID, codeWikiMCPServerName).Scan(&bindings)
		return servers, bindings
	}

	if servers, bindings := countRows(); servers != 0 || bindings != 0 {
		t.Fatalf("nothing should be registered before the first publication: %d servers, %d bindings", servers, bindings)
	}

	publish("register-one")
	servers, bindings := countRows()
	if servers != 1 || bindings != 1 {
		t.Fatalf("the first publication registers the server and binds it: %d servers, %d bindings", servers, bindings)
	}

	// Republish, twice, from two different repositories in the same workspace.
	publish("register-two")
	publish("register-three")
	servers, bindings = countRows()
	if servers != 1 || bindings != 1 {
		t.Fatalf("later publications must not duplicate: %d servers, %d bindings", servers, bindings)
	}

	var config string
	dbfx.QueryRow(t,
		`SELECT config::text FROM workspace_mcp_server WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, codeWikiMCPServerName).Scan(&config)
	if config == "" {
		t.Fatal("the registered entry needs a config the daemon can dial")
	}
}

func TestCodeWikiPublicationWithoutAnAgentStillRegistersTheServer(t *testing.T) {
	// A human-triggered publication has no agent to bind, but the workspace
	// library entry should still exist so the next agent can be given it.
	dbfx.Exec(t, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, codeWikiMCPServerName)
	dbfx.Cleanup(t, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, codeWikiMCPServerName)

	projectID, _ := wikiFixture(t, "https://github.com/acme/no-agent-"+uuid.NewString()[:6])
	snapshot := startWikiSnapshot(t, projectID, []string{"src/app.py"})
	testutil.Call(t, testHandler.CreateProjectCodeWikiPage,
		wikiRequest(t, http.MethodPost, "/x", map[string]any{
			"slug": "overview", "title": "Overview", "content": "body",
			"citations": []map[string]any{{"path": "src/app.py"}},
		}, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusCreated)
	testutil.Call(t, testHandler.PublishProjectCodeWikiSnapshot,
		wikiRequest(t, http.MethodPost, "/x", nil, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusOK)

	var servers int
	dbfx.QueryRow(t, `SELECT count(*) FROM workspace_mcp_server WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, codeWikiMCPServerName).Scan(&servers)
	if servers != 1 {
		t.Fatalf("expected the library entry to exist, got %d", servers)
	}
}
