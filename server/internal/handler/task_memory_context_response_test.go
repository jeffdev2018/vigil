package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestListAgentTasksExposesMemoryContext pins the claim-time memory selection
// on the wire. The daemon claim writes agent_task_queue.memory_context, but
// until taskToResponse carried it the transcript had no way to answer "was the
// agent given its rules for this run?" — the aggregate 30-day usage endpoint
// counts runs, it does not say which versions one run received.
//
// JSONB rejects malformed syntax, so the drift this guards is shape: a value
// that is valid JSON but not the recorded object must read as absent, not as
// an empty selection — "unknown" and "the server prepared nothing" are
// different answers.
func TestListAgentTasksExposesMemoryContext(t *testing.T) {
	agentID := createHandlerTestAgent(t, "memory-context-agent", nil)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)

	const memoryID = "11111111-1111-4111-8111-111111111111"
	const projectMemoryID = "22222222-2222-4222-8222-222222222222"
	recorded := `{"dispatched_at":"2026-09-05T10:00:00Z","agent_status":"loaded",` +
		`"agent_versions":[{"id":"` + memoryID + `","revision":4}],` +
		`"project_version":{"id":"` + projectMemoryID + `","revision":2}}`

	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":     runtimeID,
		"memory_context": recorded,
	})
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":     runtimeID,
		"memory_context": `[]`,
	})
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})

	req := newRequest("GET", "/api/agents/"+agentID+"/tasks", nil)
	req = testutil.WithURLParams(req, "id", agentID)
	w := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK)

	type memoryVersion struct {
		ID       string `json:"id"`
		Revision int32  `json:"revision"`
	}
	var tasks []struct {
		MemoryContext *struct {
			DispatchedAt   string          `json:"dispatched_at"`
			AgentStatus    string          `json:"agent_status"`
			AgentVersions  []memoryVersion `json:"agent_versions"`
			ProjectVersion *memoryVersion  `json:"project_version"`
		} `json:"memory_context"`
	}
	w.JSON(&tasks)
	if len(tasks) != 3 {
		t.Fatalf("ListAgentTasks returned %d tasks, want 3: %s", len(tasks), w.Text())
	}

	recordedCount := 0
	for _, task := range tasks {
		if task.MemoryContext == nil {
			continue
		}
		recordedCount++
		mc := task.MemoryContext
		if mc.AgentStatus != "loaded" || mc.DispatchedAt != "2026-09-05T10:00:00Z" {
			t.Fatalf("memory_context = %+v, want the recorded loaded claim", mc)
		}
		if len(mc.AgentVersions) != 1 || mc.AgentVersions[0].ID != memoryID || mc.AgentVersions[0].Revision != 4 {
			t.Fatalf("agent_versions = %+v, want one entry %s r4", mc.AgentVersions, memoryID)
		}
		if mc.ProjectVersion == nil || mc.ProjectVersion.ID != projectMemoryID || mc.ProjectVersion.Revision != 2 {
			t.Fatalf("project_version = %+v, want %s r2", mc.ProjectVersion, projectMemoryID)
		}
	}
	// The wrong-shape row and the never-claimed row both drop the field, so
	// only the well-formed one carries it.
	if recordedCount != 1 {
		t.Fatalf("%d tasks carried memory_context, want 1 (a wrong-shaped value must not become an empty selection): %s", recordedCount, w.Text())
	}
}
