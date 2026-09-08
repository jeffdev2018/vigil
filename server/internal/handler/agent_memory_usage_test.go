package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAgentMemoryUsageCoverageVersionsAndPrivacy(t *testing.T) {
	agent := agentMemoryFixture(t, "Memory usage")
	runtime := dbfx.Runtime(t, "Memory usage runtime")
	now := time.Now().UTC().Truncate(time.Microsecond)
	dispatch := now.Add(-2 * time.Hour)
	started := now.Add(-time.Hour)
	a, b := uuid.NewString(), uuid.NewString()
	receipt := func(status string, versions ...map[string]any) string {
		if versions == nil {
			versions = []map[string]any{}
		}
		data, err := json.Marshal(map[string]any{"dispatched_at": dispatch.Format(time.RFC3339Nano), "agent_status": status, "agent_versions": versions, "project_version": nil, "is_chat": false})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	version := func(id string, revision int) map[string]any { return map[string]any{"id": id, "revision": revision} }
	task := func(context any, overrides ...testutil.Cols) string {
		cols := testutil.Cols{"runtime_id": runtime, "status": "completed", "dispatched_at": dispatch, "started_at": started, "completed_at": now.Add(-30 * time.Minute), "memory_context": context, "trigger_evidence_kind": "issue_assignment"}
		for _, over := range overrides {
			for k, v := range over {
				cols[k] = v
			}
		}
		return dbfx.Task(t, agent, cols)
	}
	task(receipt("loaded", version(a, 1), version(b, 2)))
	task(receipt("loaded", version(a, 1)))
	task(receipt("loaded", version(a, 2)))
	task(receipt("loaded"))
	task(receipt("unavailable"))
	task(nil)
	task(receipt("loaded", version(a, 1)), testutil.Cols{"dispatched_at": dispatch.Add(time.Second)})
	task(receipt("future", version(a, 1)))
	task(`{"agent_status":"loaded","agent_versions":{}}`)
	// Without either a durable marker or known evidence, an old orphan could
	// be a deleted private chat. Do not expose even its aggregate count.
	task(nil, testutil.Cols{"trigger_evidence_kind": nil})
	task(`{"agent_status":"loaded","agent_versions":[]}`, testutil.Cols{"trigger_evidence_kind": nil})
	task(receipt("loaded", version(a, 1)), testutil.Cols{"status": "queued", "started_at": nil, "completed_at": nil})
	task(receipt("loaded", version(a, 1)), testutil.Cols{"started_at": now.Add(-31 * 24 * time.Hour)})
	task(receipt("loaded", version(a, 1)), testutil.Cols{"started_at": now.Add(time.Hour)})
	// Private chat runs are excluded regardless of their memory contents.
	task(receipt("loaded", version(a, 1)), testutil.Cols{"chat_session_id": dbfx.ChatSession(t, agent)})
	other := agentMemoryFixture(t, "Other memory usage")
	task(receipt("loaded", version(a, 1)), testutil.Cols{"agent_id": other})
	req := func() *http.Request {
		return withURLParam(newRequest("GET", "/api/agents/"+agent+"/memories/usage", nil), "id", agent)
	}
	var result AgentMemoryUsageResponse
	testutil.Call(t, testHandler.GetAgentMemoryUsage, req()).Want(http.StatusOK).JSON(&result)
	if result.StartedRuns != 9 || result.RecordedRuns != 5 || result.UnrecordedRuns != 4 || result.LoadFailedRuns != 1 || result.RunsWithAgentMemory != 3 {
		t.Fatalf("misleading coverage: %+v", result)
	}
	since, err := time.Parse(time.RFC3339Nano, result.Since)
	if err != nil {
		t.Fatal(err)
	}
	until, err := time.Parse(time.RFC3339Nano, result.Until)
	if err != nil {
		t.Fatal(err)
	}
	if until.Sub(since) != 30*24*time.Hour {
		t.Fatalf("wrong window: %+v", result)
	}
	if len(result.Versions) != 3 {
		t.Fatalf("wrong versions: %+v", result.Versions)
	}
	for _, row := range result.Versions {
		expected := int64(1)
		if row.MemoryID == a && row.Revision == 1 {
			expected = 2
		}
		if row.PreparedRuns != expected {
			t.Fatalf("wrong per-version count: %+v", row)
		}
		date, err := time.Parse(time.RFC3339Nano, row.LastStartedAt)
		if err != nil || !date.Equal(started) {
			t.Fatalf("wrong last start: %+v %v", row, err)
		}
	}
	// References survive deletion of memory text: there is no current memory row
	// for either ID. The counts are historical prepared contexts, not live facts.
	foreign := req()
	foreign.Header.Set("X-Workspace-ID", uuid.NewString())
	testutil.Call(t, testHandler.GetAgentMemoryUsage, foreign).Want(http.StatusNotFound)
	var empty AgentMemoryUsageResponse
	emptyAgent := agentMemoryFixture(t, "Unused memory agent")
	testutil.Call(t, testHandler.GetAgentMemoryUsage, withURLParam(newRequest("GET", "/api/agents/"+emptyAgent+"/memories/usage", nil), "id", emptyAgent)).Want(http.StatusOK).JSON(&empty)
	if empty.StartedRuns != 0 || empty.Versions == nil || len(empty.Versions) != 0 {
		t.Fatalf("empty became unknown: %+v", empty)
	}
}

func TestAgentMemoryUsageExcludesDeletedChatsAndIncludesQuickCreate(t *testing.T) {
	agent := agentMemoryFixture(t, "Memory privacy after deletion")
	runtime := dbfx.Runtime(t, "Memory privacy runtime")
	for _, chat := range []bool{true, false} {
		session := ""
		cols := testutil.Cols{"runtime_id": runtime, "status": "dispatched", "dispatched_at": time.Now().UTC()}
		if chat {
			session = dbfx.ChatSession(t, agent)
			cols["chat_session_id"] = session
		}
		id := dbfx.Task(t, agent, cols)
		row, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
		if err != nil {
			t.Fatal(err)
		}
		// Deliberately supply the opposite marker: only the database's task
		// identity may stamp privacy. Quick-create has no source relationship.
		payload, err := json.Marshal(map[string]any{"dispatched_at": row.DispatchedAt.Time.UTC().Format(time.RFC3339Nano), "agent_status": "loaded", "agent_versions": []any{}, "is_chat": !chat})
		if err != nil {
			t.Fatal(err)
		}
		updated, err := testHandler.Queries.SetTaskMemoryContext(context.Background(), db.SetTaskMemoryContextParams{TaskID: row.ID, RuntimeID: row.RuntimeID, DispatchedAt: row.DispatchedAt, MemoryContext: payload})
		if err != nil || updated != 1 {
			t.Fatalf("receipt write: %d %v", updated, err)
		}
		dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',started_at=now(),completed_at=now() WHERE id=$1`, id)
		if chat {
			req := withChatTestWorkspaceCtx(t, withURLParam(newRequest("DELETE", "/api/chat/sessions/"+session, nil), "sessionId", session))
			testutil.Call(t, testHandler.DeleteChatSession, req).Want(http.StatusNoContent)
			var remaining pgtype.UUID
			dbfx.QueryRow(t, `SELECT chat_session_id FROM agent_task_queue WHERE id=$1`, id).Scan(&remaining)
			if remaining.Valid {
				t.Fatal("chat was not detached by deletion")
			}
		}
	}
	var result AgentMemoryUsageResponse
	testutil.Call(t, testHandler.GetAgentMemoryUsage, withURLParam(newRequest("GET", "/api/agents/"+agent+"/memories/usage", nil), "id", agent)).Want(http.StatusOK).JSON(&result)
	if result.StartedRuns != 1 || result.RecordedRuns != 1 {
		t.Fatalf("deleted chat leaked or quick-create vanished: %+v", result)
	}
}

func TestAgentMemoryUsageHonorsPrivateRunHistoryGate(t *testing.T) {
	agent, owner, member := privateAgentTestFixture(t)
	for _, actor := range []struct {
		id     string
		status int
	}{{owner, http.StatusOK}, {member, http.StatusForbidden}} {
		req := withURLParam(newRequestAs(actor.id, "GET", "/api/agents/"+agent+"/memories/usage", nil), "id", agent)
		testutil.Call(t, testHandler.GetAgentMemoryUsage, req).Want(actor.status)
	}
}
