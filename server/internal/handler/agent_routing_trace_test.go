package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestListAgentTasksExposesRoutingDecision pins the runtime router's audit
// trace (JEF-237) on the wire: it is persisted on agent_task_queue.routing and
// the transcript dialog renders "why this runtime" from it, so taskToResponse
// must carry both the class the router segmented on and the decision itself.
func TestListAgentTasksExposesRoutingDecision(t *testing.T) {
	agentID := createHandlerTestAgent(t, "routing-trace-agent", nil)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)

	routing := `{"mode":"auto","task_class":"bugfix","chosen_runtime_id":"` + runtimeID +
		`","chosen_model":"claude-sonnet-4-6","reason":"best_score","candidates":[` +
		`{"runtime_id":"` + runtimeID + `","provider":"claude","model":"claude-sonnet-4-6",` +
		`"samples":42,"success_rate":0.93,"wilson_lower":0.82,"score":0.81},` +
		`{"runtime_id":"` + runtimeID + `","provider":"codex","model":"gpt-5","samples":3,` +
		`"success_rate":0.67,"wilson_lower":0.21,"excluded_reason":"too few samples"}]}`
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"task_class": "bugfix",
		"routing":    routing,
	})

	req := newRequest("GET", "/api/agents/"+agentID+"/tasks", nil)
	req = testutil.WithURLParams(req, "id", agentID)
	w := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK)

	var tasks []struct {
		TaskClass string `json:"task_class"`
		Routing   *struct {
			Mode       string `json:"mode"`
			Reason     string `json:"reason"`
			Candidates []struct {
				Model          string  `json:"model"`
				WilsonLower    float64 `json:"wilson_lower"`
				Score          float64 `json:"score"`
				ExcludedReason string  `json:"excluded_reason"`
			} `json:"candidates"`
		} `json:"routing"`
	}
	w.JSON(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("ListAgentTasks returned %d tasks, want 1: %s", len(tasks), w.Text())
	}
	task := tasks[0]
	if task.TaskClass != "bugfix" {
		t.Fatalf("task_class = %q, want bugfix", task.TaskClass)
	}
	if task.Routing == nil {
		t.Fatalf("routing decision missing from the task payload: %s", w.Text())
	}
	if task.Routing.Reason != "best_score" {
		t.Fatalf("routing.reason = %q, want best_score", task.Routing.Reason)
	}
	if len(task.Routing.Candidates) != 2 {
		t.Fatalf("routing.candidates = %d, want 2", len(task.Routing.Candidates))
	}
	if got := task.Routing.Candidates[0].WilsonLower; got != 0.82 {
		t.Fatalf("candidate wilson_lower = %v, want 0.82", got)
	}
	if got := task.Routing.Candidates[0].Score; got != 0.81 {
		t.Fatalf("candidate score = %v, want 0.81", got)
	}
	if got := task.Routing.Candidates[1].ExcludedReason; got != "too few samples" {
		t.Fatalf("candidate excluded_reason = %q, want %q", got, "too few samples")
	}
}

// A task the router never touched (fixed mode) must omit both fields rather
// than send an empty object the UI would render an empty block for.
func TestListAgentTasksOmitsRoutingForFixedMode(t *testing.T) {
	agentID := createHandlerTestAgent(t, "routing-trace-fixed-agent", nil)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})

	req := newRequest("GET", "/api/agents/"+agentID+"/tasks", nil)
	req = testutil.WithURLParams(req, "id", agentID)
	w := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK)

	var tasks []map[string]any
	w.JSON(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("ListAgentTasks returned %d tasks, want 1: %s", len(tasks), w.Text())
	}
	if _, present := tasks[0]["routing"]; present {
		t.Fatalf("fixed-mode task carries a routing key: %s", w.Text())
	}
}
