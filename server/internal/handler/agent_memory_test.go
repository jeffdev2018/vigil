package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// agentMemoryFixture builds an agent and registers memory cleanup ahead of
// the agent cleanup (t.Cleanup is LIFO), because agent_memory carries no FK
// and would otherwise orphan rows when the agent fixture is removed.
func agentMemoryFixture(t *testing.T, name string) string {
	t.Helper()
	agentID := createHandlerTestAgent(t, name, nil)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_memory WHERE agent_id = $1`, agentID)
	})
	return agentID
}

func agentMemoryRequest(method, agentID, memoryID string, body any) *http.Request {
	req := newRequest(method, "/api/agents/"+agentID+"/memories", body)
	if memoryID != "" {
		return testutil.WithURLParams(req, "id", agentID, "memoryId", memoryID)
	}
	return testutil.WithURLParams(req, "id", agentID)
}

func TestAgentMemoryCRUD(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-crud-agent")

	// Create
	w := testutil.Call(t, testHandler.CreateAgentMemory,
		agentMemoryRequest("POST", agentID, "", map[string]any{"content": "This repo uses pnpm, never npm."}))
	w.Want(http.StatusCreated)
	var created AgentMemoryResponse
	w.JSON(&created)
	if created.Content != "This repo uses pnpm, never npm." {
		t.Fatalf("CreateAgentMemory content = %q", created.Content)
	}
	if created.Source != "manual" {
		t.Fatalf("CreateAgentMemory source = %q, want manual", created.Source)
	}
	if created.AgentID != agentID {
		t.Fatalf("CreateAgentMemory agent_id = %q, want %q", created.AgentID, agentID)
	}

	// List
	w = testutil.Call(t, testHandler.ListAgentMemories,
		agentMemoryRequest("GET", agentID, "", nil))
	w.Want(http.StatusOK)
	var listed []AgentMemoryResponse
	w.JSON(&listed)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("ListAgentMemories = %#v, want the single created row", listed)
	}

	// Update
	w = testutil.Call(t, testHandler.UpdateAgentMemory,
		agentMemoryRequest("PUT", agentID, created.ID, map[string]any{"content": "This repo uses pnpm workspaces."}))
	w.Want(http.StatusOK)
	var updated AgentMemoryResponse
	w.JSON(&updated)
	if updated.Content != "This repo uses pnpm workspaces." {
		t.Fatalf("UpdateAgentMemory content = %q", updated.Content)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Fatalf("UpdateAgentMemory changed created_at: %q -> %q", created.CreatedAt, updated.CreatedAt)
	}

	// Delete
	w = testutil.Call(t, testHandler.DeleteAgentMemory,
		agentMemoryRequest("DELETE", agentID, created.ID, nil))
	w.Want(http.StatusNoContent)

	var count int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM agent_memory WHERE id = $1`, created.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("DeleteAgentMemory returned 204 but row still exists (count=%d)", count)
	}

	// List after delete is empty
	w = testutil.Call(t, testHandler.ListAgentMemories,
		agentMemoryRequest("GET", agentID, "", nil))
	w.Want(http.StatusOK)
	listed = nil
	w.JSON(&listed)
	if len(listed) != 0 {
		t.Fatalf("ListAgentMemories after delete = %#v, want empty", listed)
	}
}

func TestAgentMemoryCrossWorkspaceIs404(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-cross-ws-agent")

	otherWorkspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Memory cross-workspace test",
		"slug":         "memory-xws-test",
		"description":  "Foreign workspace",
		"issue_prefix": "MXW",
	})

	// The agent exists but belongs to another workspace: every verb must 404,
	// never leak existence.
	req := newRequest("GET", "/api/agents/"+agentID+"/memories", nil)
	req.Header.Set("X-Workspace-ID", otherWorkspaceID)
	req = testutil.WithURLParams(req, "id", agentID)
	testutil.Call(t, testHandler.ListAgentMemories, req).Want(http.StatusNotFound)

	req = newRequest("POST", "/api/agents/"+agentID+"/memories", map[string]any{"content": "foreign write"})
	req.Header.Set("X-Workspace-ID", otherWorkspaceID)
	req = testutil.WithURLParams(req, "id", agentID)
	testutil.Call(t, testHandler.CreateAgentMemory, req).Want(http.StatusNotFound)
}

func TestAgentMemoryValidation(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-validation-agent")

	for _, content := range []string{"", "   \n\t  "} {
		w := testutil.Call(t, testHandler.CreateAgentMemory,
			agentMemoryRequest("POST", agentID, "", map[string]any{"content": content}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("CreateAgentMemory content %q: expected 400, got %d: %s", content, w.Code, w.Body.String())
		}
	}

	// 500 runes pass, 501 are refused — the boundary the table CHECK enforces.
	w := testutil.Call(t, testHandler.CreateAgentMemory,
		agentMemoryRequest("POST", agentID, "", map[string]any{"content": strings.Repeat("a", 500)}))
	w.Want(http.StatusCreated)

	w = testutil.Call(t, testHandler.CreateAgentMemory,
		agentMemoryRequest("POST", agentID, "", map[string]any{"content": strings.Repeat("a", 501)}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAgentMemory 501 chars: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentMemoryCap409(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-cap-agent")

	// Seed the agent at the 200-fact cap in one statement; content varies so
	// the rows are distinct facts.
	dbfx.Exec(t, `
		INSERT INTO agent_memory (workspace_id, agent_id, content, source)
		SELECT $1, $2, 'seeded fact ' || g, 'manual'
		FROM generate_series(1, 200) AS g
	`, testWorkspaceID, agentID)

	w := testutil.Call(t, testHandler.CreateAgentMemory,
		agentMemoryRequest("POST", agentID, "", map[string]any{"content": "one fact too many"}))
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateAgentMemory at cap: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "limit") {
		t.Fatalf("CreateAgentMemory at cap: expected a clear limit message, got %s", w.Body.String())
	}

	var count int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM agent_memory WHERE agent_id = $1`, agentID).Scan(&count)
	if count != agentMemoryMaxPerAgent {
		t.Fatalf("rejected create still wrote a row (count=%d, want %d)", count, agentMemoryMaxPerAgent)
	}
}

// TestAgentMemoryRejectsForeignMemory covers the {memoryId} guard: a memory
// that exists in the workspace but belongs to ANOTHER agent must 404 on the
// nested routes, so one agent's facts cannot be edited through a sibling's URL.
func TestAgentMemoryRejectsForeignMemory(t *testing.T) {
	ownerID := agentMemoryFixture(t, "memory-owner-agent")
	otherID := agentMemoryFixture(t, "memory-other-agent")

	w := testutil.Call(t, testHandler.CreateAgentMemory,
		agentMemoryRequest("POST", ownerID, "", map[string]any{"content": "owner agent fact"}))
	w.Want(http.StatusCreated)
	var created AgentMemoryResponse
	w.JSON(&created)

	for _, verb := range []struct {
		method  string
		handler func(http.ResponseWriter, *http.Request)
		body    any
	}{
		{"PUT", testHandler.UpdateAgentMemory, map[string]any{"content": "hijacked"}},
		{"DELETE", testHandler.DeleteAgentMemory, nil},
	} {
		w := testutil.Call(t, verb.handler,
			agentMemoryRequest(verb.method, otherID, created.ID, verb.body))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s memory of another agent: expected 404, got %d: %s", verb.method, w.Code, w.Body.String())
		}
	}

	var content string
	dbfx.QueryRow(t, `SELECT content FROM agent_memory WHERE id = $1`, created.ID).Scan(&content)
	if content != "owner agent fact" {
		t.Fatalf("foreign-agent write reached the row: content = %q", content)
	}
}

// TestClaimTaskIncludesAgentMemories pins the single assembly point: a claimed
// task's agent payload must carry the agent's memory facts (JEF-236), in
// chronological order, so the daemon can render the Memory section without a
// second round trip.
func TestClaimTaskIncludesAgentMemories(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := agentMemoryFixture(t, "memory-claim-agent")
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)

	older := dbfx.Insert(t, "agent_memory", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"agent_id":     agentID,
		"content":      "This repo uses pnpm, never npm.",
		"created_at":   testutil.Raw("now() - interval '1 hour'"),
	})
	dbfx.Insert(t, "agent_memory", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"agent_id":     agentID,
		"content":      "Run make test before pushing.",
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_memory WHERE agent_id = $1`, agentID)
	})
	_ = older

	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, "test-claim-agent-memory")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			Agent *struct {
				Memories []string `json:"memories"`
			} `json:"agent"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil || resp.Task.Agent == nil {
		t.Fatalf("claim response missing task.agent: %s", w.Text())
	}
	want := []string{"This repo uses pnpm, never npm.", "Run make test before pushing."}
	got := resp.Task.Agent.Memories
	if len(got) != len(want) {
		t.Fatalf("claim agent.memories = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("claim agent.memories[%d] = %q, want %q (chronological order)", i, got[i], want[i])
		}
	}
}

// TestAgentMemoryWorkspaceDeleteSweep pins the no-FK cleanup: deleting the
// workspace removes its agent_memory rows in the same transaction as the
// agents themselves.
func TestAgentMemoryWorkspaceDeleteSweep(t *testing.T) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	workspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Memory sweep test",
		"slug":         "memory-sweep-" + suffix,
		"description":  "sweep",
		"issue_prefix": "MSW",
	})
	dbfx.Insert(t, "member", testutil.Cols{
		"workspace_id": workspaceID,
		"user_id":      testUserID,
		"role":         "owner",
	})
	runtimeID := dbfx.Runtime(t, "memory-sweep-runtime", testutil.Cols{"workspace_id": workspaceID})
	agentID := dbfx.Agent(t, "memory-sweep-agent", runtimeID, testutil.Cols{
		"workspace_id": workspaceID,
		"owner_id":     testUserID,
	})
	dbfx.Insert(t, "agent_memory", testutil.Cols{
		"workspace_id": workspaceID,
		"agent_id":     agentID,
		"content":      "swept with the workspace",
	})

	req := newRequest("DELETE", "/api/workspaces/"+workspaceID, nil)
	req = testutil.WithURLParams(req, "id", workspaceID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)

	var count int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM agent_memory WHERE workspace_id = $1`, workspaceID).Scan(&count)
	if count != 0 {
		t.Fatalf("workspace delete left %d agent_memory rows behind", count)
	}
}
