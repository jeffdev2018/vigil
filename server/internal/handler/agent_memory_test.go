package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// agentMemoryFixture builds an agent and registers memory cleanup ahead of
// the agent cleanup (t.Cleanup is LIFO), because agent_memory carries no FK
// and would otherwise orphan rows when the agent fixture is removed.
func agentMemoryFixture(t *testing.T, name string) string {
	t.Helper()
	agentID := createHandlerTestAgent(t, name, nil)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_memory_version WHERE agent_id = $1`, agentID)
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
		testPool.Exec(context.Background(), `DELETE FROM agent_memory_version WHERE agent_id = $1`, agentID)
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
	createReq := agentMemoryRequest("POST", agentID, "", map[string]any{"content": "swept with the workspace"})
	createReq.Header.Set("X-Workspace-ID", workspaceID)
	testutil.Call(t, testHandler.CreateAgentMemory, createReq).Want(http.StatusCreated)

	req := newRequest("DELETE", "/api/workspaces/"+workspaceID, nil)
	req = testutil.WithURLParams(req, "id", workspaceID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)

	var count int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM agent_memory WHERE workspace_id = $1`, workspaceID).Scan(&count)
	if count != 0 {
		t.Fatalf("workspace delete left %d agent_memory rows behind", count)
	}
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM agent_memory_version WHERE workspace_id = $1`, workspaceID).Scan(&count)
	if count != 0 {
		t.Fatalf("workspace delete left %d versions", count)
	}
}

func TestAgentMemoryReviewControlsBriefAndRejectsStaleApproval(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-review")
	memoryID := dbfx.Insert(t, "agent_memory", testutil.Cols{
		"workspace_id": testWorkspaceID, "agent_id": agentID,
		"content": "Use pnpm.", "source": "run", "status": "pending",
	})
	params := db.ListRecentAgentMemoriesParams{AgentID: util.MustParseUUID(agentID), WorkspaceID: util.MustParseUUID(testWorkspaceID)}
	assertInjected := func(want int) {
		t.Helper()
		rows, err := testHandler.Queries.ListRecentAgentMemories(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != want {
			t.Fatalf("brief memories = %d, want %d", len(rows), want)
		}
	}
	assertInjected(0)
	var edited AgentMemoryResponse
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID,
		map[string]any{"content": "Use pnpm, never npm.", "expected_revision": 1})).Want(http.StatusOK).JSON(&edited)
	if edited.Status != "pending" || edited.Revision != 2 {
		t.Fatalf("editing approved suggestion: %+v", edited)
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID,
		map[string]any{"status": "active", "expected_revision": 1})).Want(http.StatusConflict)
	assertInjected(0)
	var approved AgentMemoryResponse
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID,
		map[string]any{"status": "active", "expected_revision": 2})).Want(http.StatusOK).JSON(&approved)
	if approved.ReviewedBy == nil || *approved.ReviewedBy != testUserID || approved.ReviewedAt == nil {
		t.Fatalf("missing human review attribution: %+v", approved)
	}
	assertInjected(1)
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID,
		map[string]any{"status": "rejected", "expected_revision": 3})).Want(http.StatusOK)
	assertInjected(0)
	var rows []AgentMemoryResponse
	testutil.Call(t, testHandler.ListAgentMemories, agentMemoryRequest("GET", agentID, "", nil)).Want(http.StatusOK).JSON(&rows)
	if len(rows) != 1 || rows[0].Status != "rejected" {
		t.Fatalf("rejected suggestion must stay available: %+v", rows)
	}
}

func TestAgentMemoryReviewRequiresHumanAndRevision(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-human-review")
	memoryID := dbfx.Insert(t, "agent_memory", testutil.Cols{
		"workspace_id": testWorkspaceID, "agent_id": agentID, "content": "Use pnpm.", "status": "pending",
	})
	for _, source := range []string{"task_token", "cloud_pat"} {
		for _, op := range []struct {
			method  string
			handler http.HandlerFunc
		}{
			{"POST", testHandler.CreateAgentMemory}, {"PUT", testHandler.UpdateAgentMemory}, {"DELETE", testHandler.DeleteAgentMemory},
		} {
			req := agentMemoryRequest(op.method, agentID, memoryID, map[string]any{"content": "Bypass review", "status": "active", "expected_revision": 1})
			req.Header.Set("X-Actor-Source", source)
			testutil.Call(t, op.handler, req).Want(http.StatusForbidden)
		}
	}
	for _, body := range []map[string]any{
		{"status": "active"}, {"status": "invented", "expected_revision": 1}, {"status": "active", "expected_revision": 0},
	} {
		testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, body)).Want(http.StatusBadRequest)
	}
}

func TestAgentMemoryCorrectionKeepsSourceAndRequiresReview(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-correction")
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"status": "failed", "runtime_id": runtimeID})
	var correction AgentMemoryResponse
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agentID, "", map[string]any{
		"content": "Use the project timezone when interpreting dates.", "source_task_id": taskID,
	})).Want(http.StatusCreated).JSON(&correction)
	if correction.Status != "pending" || correction.Source != "manual" || correction.SourceTaskID == nil || *correction.SourceTaskID != taskID {
		t.Fatalf("correction lost provenance or became active: %+v", correction)
	}
	otherAgentID := agentMemoryFixture(t, "memory-foreign-source")
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", otherAgentID, "", map[string]any{
		"content": "Foreign correction", "source_task_id": taskID,
	})).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agentID, "", map[string]any{
		"content": "Invalid source", "source_task_id": "not-a-uuid",
	})).Want(http.StatusBadRequest)
}

func TestAgentMemoryExpirationPreservesReviewAndSource(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-expiry")
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	var memory AgentMemoryResponse
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agentID, "", map[string]any{"content": "Temporary endpoint", "expires_at": future})).Want(http.StatusCreated).JSON(&memory)
	if memory.ExpiresAt == nil || memory.Expired {
		t.Fatalf("expiry missing: %+v", memory)
	}
	// Expiration is time-based and does not delete the reviewed content.
	dbfx.Exec(t, `UPDATE agent_memory SET expires_at=now()-interval '1 hour' WHERE id=$1`, memory.ID)
	params := db.ListRecentAgentMemoriesParams{AgentID: util.MustParseUUID(agentID), WorkspaceID: util.MustParseUUID(testWorkspaceID)}
	rows, err := testHandler.Queries.ListRecentAgentMemories(context.Background(), params)
	if err != nil || len(rows) != 0 {
		t.Fatalf("expired fact injected: %v %v", rows, err)
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memory.ID, map[string]any{"status": "active", "expected_revision": 1})).Want(http.StatusOK).JSON(&memory)
	if !memory.Expired {
		t.Fatal("approval silently renewed expiration")
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memory.ID, map[string]any{"content": "Still expired", "expected_revision": 2})).Want(http.StatusOK).JSON(&memory)
	if !memory.Expired {
		t.Fatal("content edit silently renewed expiration")
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memory.ID, map[string]any{"expires_at": future, "expected_revision": 1})).Want(http.StatusConflict)
	for _, body := range []map[string]any{
		{"expires_at": future},
		{"expires_at": "invalid", "expected_revision": 3},
		{"expires_at": "2000-01-01T00:00:00Z", "expected_revision": 3},
	} {
		testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memory.ID, body)).Want(http.StatusBadRequest)
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memory.ID, map[string]any{"expires_at": nil, "expected_revision": 3})).Want(http.StatusOK).JSON(&memory)
	if memory.Expired || memory.ExpiresAt != nil || memory.Content != "Still expired" || memory.Revision != 4 {
		t.Fatalf("explicit renewal: %+v", memory)
	}
	rows, err = testHandler.Queries.ListRecentAgentMemories(context.Background(), params)
	if err != nil || len(rows) != 1 {
		t.Fatalf("renewed fact missing: %v %v", rows, err)
	}
	// Setting an expiry is not approval of a pending suggestion.
	dbfx.Exec(t, `UPDATE agent_memory SET status='pending' WHERE id=$1`, memory.ID)
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memory.ID, map[string]any{"expires_at": future, "expected_revision": 4})).Want(http.StatusOK).JSON(&memory)
	if memory.Status != "pending" {
		t.Fatalf("expiry approved a suggestion: %+v", memory)
	}
	rows, err = testHandler.Queries.ListRecentAgentMemories(context.Background(), params)
	if err != nil || len(rows) != 0 {
		t.Fatalf("pending fact injected: %v %v", rows, err)
	}
}

func TestAgentMemoryHistoryRestoreAndBoundaries(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-history")
	// A pre-existing expired suggestion is preserved on its first edit.
	memoryID := dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentID, "content": "Original rule", "source": "run", "status": "pending", "expires_at": time.Now().Add(-time.Hour)})
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"content": "Replacement rule", "status": "active", "expires_at": nil, "expected_revision": 1})).Want(http.StatusOK)
	var restored AgentMemoryResponse
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"restore_revision": 1, "expected_revision": 2})).Want(http.StatusOK).JSON(&restored)
	if restored.Content != "Original rule" || restored.Status != "pending" || !restored.Expired || restored.Revision != 3 || restored.Source != "run" || restored.ReviewedBy == nil || *restored.ReviewedBy != testUserID {
		t.Fatalf("restore contract: %+v", restored)
	}
	var history AgentMemoryHistoryResponse
	testutil.Call(t, testHandler.ListAgentMemoryHistory, agentMemoryRequest("GET", agentID, memoryID, nil)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 3 || history.NextBeforeRevision != nil || history.Versions[0].RestoredFromRevision == nil || *history.Versions[0].RestoredFromRevision != 1 || history.Versions[2].Content != "Original rule" {
		t.Fatalf("history: %+v", history)
	}
	for _, body := range []map[string]any{
		{"restore_revision": 1}, {"restore_revision": 0, "expected_revision": 3},
		{"restore_revision": 1, "expected_revision": 3, "content": "Other"},
		{"restore_revision": 1, "expected_revision": 3, "expires_at": nil},
		{"restore_revision": 1, "expected_revision": 3, "status": "active"},
	} {
		testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, body)).Want(http.StatusBadRequest)
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"restore_revision": 1, "expected_revision": 2})).Want(http.StatusConflict)
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"restore_revision": 999, "expected_revision": 3})).Want(http.StatusNotFound)
	foreignAgent := agentMemoryFixture(t, "memory-history-foreign")
	testutil.Call(t, testHandler.ListAgentMemoryHistory, agentMemoryRequest("GET", foreignAgent, memoryID, nil)).Want(http.StatusNotFound)
	req := agentMemoryRequest("GET", agentID, memoryID, nil)
	req.Header.Set("X-User-ID", dbfx.User(t, "History outsider", "agent-memory-outsider@example.test"))
	// Exercise the membership middleware used by the real route.
	guarded := middleware.RequireWorkspaceMember(testHandler.Queries)(http.HandlerFunc(testHandler.ListAgentMemoryHistory))
	testutil.Call(t, guarded.ServeHTTP, req).Want(http.StatusNotFound)
	req = agentMemoryRequest("GET", agentID, memoryID, nil)
	req.URL.RawQuery = "before_revision=bad"
	testutil.Call(t, testHandler.ListAgentMemoryHistory, req).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.DeleteAgentMemory, agentMemoryRequest("DELETE", agentID, memoryID, nil)).Want(http.StatusNoContent)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_memory_version WHERE memory_id=$1`, memoryID).Scan(&count)
	if count != 0 {
		t.Fatal("delete left history behind")
	}
}

func TestAgentMemoryHistoryConcurrencyRollbackAndPagination(t *testing.T) {
	agentID := agentMemoryFixture(t, "memory-history-cas")
	var memory AgentMemoryResponse
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agentID, "", map[string]any{"content": "Initial"})).Want(http.StatusCreated).JSON(&memory)
	codes := make(chan int, 2)
	for range 2 {
		go func() {
			w := httptest.NewRecorder()
			testHandler.UpdateAgentMemory(w, agentMemoryRequest("PUT", agentID, memory.ID, map[string]any{"content": "Winner", "expected_revision": 1}))
			codes <- w.Code
		}()
	}
	a, b := <-codes, <-codes
	if !(a == 200 && b == 409 || a == 409 && b == 200) {
		t.Fatalf("CAS %d %d", a, b)
	}
	original := testHandler.TxStarter
	t.Cleanup(func() { testHandler.TxStarter = original })
	testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memory.ID, map[string]any{"restore_revision": 1, "expected_revision": 2})).Want(http.StatusInternalServerError)
	testHandler.TxStarter = original
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_memory_version WHERE memory_id=$1`, memory.ID).Scan(&count)
	if count != 2 {
		t.Fatalf("rollback leaked versions: %d", count)
	}
	for revision := 2; revision <= 22; revision++ {
		testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memory.ID, map[string]any{"content": fmt.Sprintf("Rule %d", revision), "expected_revision": revision})).Want(http.StatusOK)
	}
	var history AgentMemoryHistoryResponse
	testutil.Call(t, testHandler.ListAgentMemoryHistory, agentMemoryRequest("GET", agentID, memory.ID, nil)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 20 || history.NextBeforeRevision == nil || *history.NextBeforeRevision != 4 {
		t.Fatalf("first page %+v", history)
	}
	req := agentMemoryRequest("GET", agentID, memory.ID, nil)
	req.URL.RawQuery = "before_revision=4"
	testutil.Call(t, testHandler.ListAgentMemoryHistory, req).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 3 || history.NextBeforeRevision != nil || history.Versions[2].Content != "Initial" {
		t.Fatalf("tail %+v", history)
	}
}

func TestAgentMemoryHistoryCarrierDeletion(t *testing.T) {
	session := newBuilderSession(t)
	memoryID := dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": session.BuilderAgentID, "content": "Carrier rule"})
	if err := testHandler.Queries.SaveAgentMemoryVersion(context.Background(), db.SaveAgentMemoryVersionParams{MemoryID: parseUUID(memoryID), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatal(err)
	}
	req := testutil.WithURLParams(newRequest("DELETE", "/api/chat/sessions/"+session.SessionID, nil), "sessionId", session.SessionID)
	testutil.Call(t, testHandler.DeleteChatSession, withChatTestWorkspaceCtx(t, req)).Want(http.StatusNoContent)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_memory_version WHERE memory_id=$1`, memoryID).Scan(&count)
	if count != 0 {
		t.Fatal("carrier deletion left memory history")
	}
}
