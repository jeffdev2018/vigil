package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestUpdateAgentRejectsInvalidStatus covers the audit finding that
// UpdateAgent wrote req.Status straight into params.Status with no enum
// validation, unlike MaxConcurrentTasks right below it. An invalid value
// violated the agent.status CHECK constraint and surfaced as a 500 with a
// raw Postgres error message instead of a clean 400.
func TestUpdateAgentRejectsInvalidStatus(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Status Validation Test", nil)

	req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{"status": "bogus"})
	req = withURLParam(req, "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusBadRequest)

	var status string
	if err := testPool.QueryRow(t.Context(), `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&status); err != nil {
		t.Fatalf("read agent status: %v", err)
	}
	if status == "bogus" {
		t.Fatal("invalid status must not reach the database")
	}

	var out AgentResponse
	req = newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{"status": "blocked"})
	req = withURLParam(req, "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusOK).JSON(&out)
}
