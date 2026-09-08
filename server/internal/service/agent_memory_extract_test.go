package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// agentMemoryExtractionFixture seeds one workspace/agent/issue plus a terminal
// task row for the extraction pass to read.
type agentMemoryExtractionFixture struct {
	workspaceID string
	userID      string
	agentID     string
	issueID     string
}

func seedAgentMemoryExtractionFixture(t *testing.T) (agentMemoryExtractionFixture, *pgxpool.Pool) {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_memory_version WHERE agent_id=$1`, agentID) })
	return agentMemoryExtractionFixture{
		workspaceID: workspaceID,
		userID:      userID,
		agentID:     agentID,
		issueID:     issueID,
	}, pool
}

// seedTerminalTask inserts one task row for the fixture's agent with the given
// status and final output, mirroring what CompleteTask persists.
func (f agentMemoryExtractionFixture) seedTerminalTask(t *testing.T, pool *pgxpool.Pool, status, output string) string {
	t.Helper()
	result, err := json.Marshal(protocol.TaskCompletedPayload{Output: output})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var runtimeID string
	if err := pool.QueryRow(context.Background(),
		`SELECT runtime_id::text FROM agent WHERE id = $1`, f.agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, result,
			originator_user_id, accountable_user_id, originator_source
		)
		VALUES ($1, $2, $3, $4, 0, $5::jsonb, $6, $6, 'direct_human')
		RETURNING id`, f.agentID, runtimeID, f.issueID, status, result, f.userID).Scan(&taskID); err != nil {
		t.Fatalf("seed terminal task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func (f agentMemoryExtractionFixture) memoryContents(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT content, source FROM agent_memory WHERE agent_id = $1 ORDER BY created_at ASC`, f.agentID)
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var content, source string
		if err := rows.Scan(&content, &source); err != nil {
			t.Fatalf("scan memory: %v", err)
		}
		out = append(out, source+": "+content)
	}
	return out
}

// stubMemoryLLM returns an enabled *llm.Client pointed at an httptest server
// answering every chat completion with the given JSON payload.
func stubMemoryLLM(t *testing.T, reply string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		content, _ := json.Marshal(reply)
		fmt.Fprintf(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}]}`, content)
	}))
	t.Cleanup(srv.Close)
	retries, err := llm.Retries(0)
	if err != nil {
		t.Fatalf("retries: %v", err)
	}
	return llm.New(llm.Config{APIKey: "test", BaseURL: srv.URL, MaxRetries: retries})
}

func memoryExtractionService(pool *pgxpool.Pool, bus *events.Bus, client AgentMemoryLLM) *TaskService {
	return &TaskService{
		Queries:          db.New(pool),
		TxStarter:        pool,
		Bus:              bus,
		MemoryExtraction: client,
	}
}

func TestAgentMemoryExtractionInsertsFacts(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Migrated the build to pnpm workspaces; `make test` runs the Go suite.")
	svc := memoryExtractionService(pool, events.New(),
		stubMemoryLLM(t, `{"facts":["This repo uses pnpm workspaces.","Run make test for the Go suite."]}`))

	if err := svc.ExtractAgentMemoriesForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract: %v", err)
	}

	got := fx.memoryContents(t, pool)
	want := []string{"run: This repo uses pnpm workspaces.", "run: Run make test for the Go suite."}
	if len(got) != len(want) {
		t.Fatalf("memories = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("memory[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// The provenance link back to the run is what lets a human audit where a
	// fact came from.
	var linked int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_memory WHERE agent_id = $1 AND source_task_id = $2 AND status = 'pending'`,
		fx.agentID, taskID).Scan(&linked); err != nil {
		t.Fatalf("count linked: %v", err)
	}
	if linked != 2 {
		t.Fatalf("source_task_id linked rows = %d, want 2", linked)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_memory_version WHERE agent_id=$1 AND source_task_id=$2 AND status='pending' AND revision=1`, fx.agentID, taskID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 2 {
		t.Fatalf("missing extraction versions: %d", linked)
	}
}

// TestAgentMemoryExtractionInsertsDrafts pins the governance default
// (JEF-269): facts the pass learned on its own land as drafts until a human
// approves them, while manual rows stay approved.
func TestAgentMemoryExtractionInsertsDrafts(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Migrated the build to pnpm workspaces.")
	svc := memoryExtractionService(pool, events.New(),
		stubMemoryLLM(t, `{"facts":["This repo uses pnpm workspaces."]}`))

	if err := svc.ExtractAgentMemoriesForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract: %v", err)
	}

	var state string
	if err := pool.QueryRow(context.Background(),
		`SELECT state FROM agent_memory WHERE agent_id = $1 AND source = 'run'`, fx.agentID).Scan(&state); err != nil {
		t.Fatalf("load extracted memory state: %v", err)
	}
	if state != "draft" {
		t.Fatalf("extracted memory state = %q, want draft", state)
	}
}

func TestAgentMemoryExtractionDisabledLLMIsNoop(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Some output worth mining.")
	svc := memoryExtractionService(pool, events.New(), llm.New(llm.Config{}))

	if err := svc.ExtractAgentMemoriesForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract with disabled LLM: %v", err)
	}
	if got := fx.memoryContents(t, pool); len(got) != 0 {
		t.Fatalf("disabled LLM still wrote memories: %v", got)
	}
}

func TestAgentMemoryExtractionDedupsAgainstExisting(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Confirmed the package manager choice again.")

	// A human already taught this fact; the model restating it in different
	// case/whitespace must not produce a second row.
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO agent_memory (workspace_id, agent_id, content, source)
		VALUES ($1, $2, 'This repo uses pnpm, never npm.', 'manual')`,
		fx.workspaceID, fx.agentID); err != nil {
		t.Fatalf("seed manual memory: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_memory WHERE agent_id = $1`, fx.agentID)
	})

	svc := memoryExtractionService(pool, events.New(),
		stubMemoryLLM(t, `{"facts":["this  repo   uses pnpm, never npm.","CI runs on every push."]}`))

	if err := svc.ExtractAgentMemoriesForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := fx.memoryContents(t, pool)
	want := []string{"manual: This repo uses pnpm, never npm.", "run: CI runs on every push."}
	if len(got) != len(want) {
		t.Fatalf("memories = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("memory[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAgentMemoryExtractionCapsAtThreeFacts(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "A very fruitful run.")
	svc := memoryExtractionService(pool, events.New(),
		stubMemoryLLM(t, `{"facts":["Fact one.","Fact two.","Fact three.","Fact four.","Fact five."]}`))

	if err := svc.ExtractAgentMemoriesForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := fx.memoryContents(t, pool)
	if len(got) != agentMemoryExtractionMaxFacts {
		t.Fatalf("inserted %d facts, want cap of %d: %v", len(got), agentMemoryExtractionMaxFacts, got)
	}
}

func TestAgentMemoryExtractionSkipsPrivateChat(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	chatID := testutil.New(pool, fx.workspaceID, fx.userID).ChatSession(t, fx.agentID)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Private conversation output.")
	if _, err := pool.Exec(context.Background(), `UPDATE agent_task_queue SET chat_session_id = $1, issue_id = NULL WHERE id = $2`, chatID, taskID); err != nil {
		t.Fatal(err)
	}
	svc := memoryExtractionService(pool, events.New(),
		stubMemoryLLM(t, `{"facts":["Private fact must not become shared memory."]}`))
	if err := svc.ExtractAgentMemoriesForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	if got := fx.memoryContents(t, pool); len(got) != 0 {
		t.Fatalf("private chat leaked into memory: %v", got)
	}
}

func TestAgentMemoryExtractionSkipsNonSuccessRun(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	// A context-exhausted run is re-routed to the failure path, so its output
	// (the provider's "context window is full" notice) must never be mined.
	taskID := fx.seedTerminalTask(t, pool, "failed", "Your context window is full.")
	svc := memoryExtractionService(pool, events.New(),
		stubMemoryLLM(t, `{"facts":["Should never be persisted."]}`))

	if err := svc.ExtractAgentMemoriesForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract on failed task: %v", err)
	}
	if got := fx.memoryContents(t, pool); len(got) != 0 {
		t.Fatalf("failed run still produced memories: %v", got)
	}
}

// TestAgentMemoryExtractionEventWiring drives the async path end to end: a
// task:completed event on the bus must land facts in agent_memory without the
// publisher waiting for the pass.
func TestAgentMemoryExtractionEventWiring(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Set up the deploy pipeline.")

	bus := events.New()
	svc := memoryExtractionService(pool, bus,
		stubMemoryLLM(t, `{"facts":["Deploys go through the Helm chart."]}`))
	svc.SubscribeAgentMemoryExtraction(bus)

	bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: fx.workspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  taskID,
			"agent_id": fx.agentID,
			"status":   "completed",
		},
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := fx.memoryContents(t, pool); len(got) == 1 {
			if got[0] != "run: Deploys go through the Helm chart." {
				t.Fatalf("memory = %q", got[0])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the extraction pass to write its fact")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAgentMemoryExtractionPreservesReviewedAndRejectedFactsAtCapacity(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "New conventions.")
	fixtures := testutil.New(pool, fx.workspaceID, fx.userID)
	for i := 0; i < 200; i++ {
		status := "active"
		if i == 199 {
			status = "rejected"
		}
		fixtures.Insert(t, "agent_memory", testutil.Cols{
			"workspace_id": fx.workspaceID, "agent_id": fx.agentID,
			"content": fmt.Sprintf("Keep fact %d.", i), "source": "run", "status": status,
		})
	}
	svc := memoryExtractionService(pool, events.New(), stubMemoryLLM(t, `{"facts":["New suggestion."]}`))
	if err := svc.ExtractAgentMemoriesForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	contents := fx.memoryContents(t, pool)
	if len(contents) != 200 {
		t.Fatalf("capacity changed: %d", len(contents))
	}
	for _, content := range contents {
		if content == "run: New suggestion." {
			t.Fatal("extraction displaced a reviewed or rejected fact")
		}
	}
}
