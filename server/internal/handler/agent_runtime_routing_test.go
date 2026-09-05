package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestUpdateAgentRuntimeRouting covers the JEF-237 opt-in: "auto" and "fixed"
// are accepted and serialized back, any other value is a 400, and switching
// modes never moves the agent's bound runtime (the auto-mode fallback).
func TestUpdateAgentRuntimeRouting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "routing-mode-agent", nil)

	// Invalid value → 400.
	badReq := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"runtime_routing": "smart",
	}), "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, badReq).Want(http.StatusBadRequest)

	// Valid value → 200, serialized, bound runtime untouched.
	var boundRuntime string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&boundRuntime)
	req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"runtime_routing": "auto",
	}), "id", agentID)
	resp := testutil.Decode[AgentResponse](t, testHandler.UpdateAgent, req, http.StatusOK)
	if resp.RuntimeRouting != "auto" {
		t.Errorf("runtime_routing = %q, want auto", resp.RuntimeRouting)
	}
	if resp.RuntimeID != boundRuntime {
		t.Errorf("runtime_id = %q, want unchanged %q (auto keeps the fallback)", resp.RuntimeID, boundRuntime)
	}

	// Back to fixed → 200.
	fixedReq := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"runtime_routing": "fixed",
	}), "id", agentID)
	resp = testutil.Decode[AgentResponse](t, testHandler.UpdateAgent, fixedReq, http.StatusOK)
	if resp.RuntimeRouting != "fixed" {
		t.Errorf("runtime_routing = %q, want fixed", resp.RuntimeRouting)
	}
}

// TestGetRuntimeRoutingStats seeds terminal runs with usage and checks the
// endpoint's aggregation: success rate, averages, and the runtime name join.
func TestGetRuntimeRoutingStats(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "routing-stats-agent", nil)
	runtimeID := handlerTestRuntimeID(t)
	for i := 0; i < 4; i++ {
		status := "completed"
		if i == 3 {
			status = "failed"
		}
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id":   runtimeID,
			"status":       status,
			"task_class":   "bugfix",
			"started_at":   testutil.Raw("now() - interval '2 minutes'"),
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
		})
		dbfx.Insert(t, "task_usage", testutil.Cols{
			"task_id":        taskID,
			"provider":       "OpenAI",
			"model":          "gpt-test",
			"cost_usd_ticks": 2_000_000_000, // $0.20
		})
	}

	req := newRequest(http.MethodGet, "/api/runtimes/routing-stats", nil)
	resp := testutil.Decode[RuntimeRoutingStatsResponse](t, testHandler.GetRuntimeRoutingStats, req, http.StatusOK)
	if resp.WindowDays != routingStatsWindowDays {
		t.Errorf("window_days = %d, want %d", resp.WindowDays, routingStatsWindowDays)
	}
	var row *RuntimeRoutingStatsRow
	for i := range resp.Rows {
		r := &resp.Rows[i]
		if r.RuntimeID == runtimeID && r.Model == "gpt-test" && r.TaskClass == "bugfix" {
			row = r
			break
		}
	}
	if row == nil {
		t.Fatalf("no stats row for the seeded (runtime, model, class): %+v", resp.Rows)
	}
	if row.RuntimeName == "" {
		t.Error("runtime_name is empty — the agent_runtime join failed")
	}
	if row.Provider != "openai" {
		t.Errorf("provider = %q, want lowercased openai", row.Provider)
	}
	if row.Samples != 4 {
		t.Errorf("samples = %d, want 4", row.Samples)
	}
	if row.SuccessRate != 0.75 {
		t.Errorf("success_rate = %f, want 0.75", row.SuccessRate)
	}
	if row.AvgCostUSD == nil || *row.AvgCostUSD != 0.20 {
		t.Errorf("avg_cost_usd = %v, want 0.20", row.AvgCostUSD)
	}
	if row.AvgDurationSecs == nil || *row.AvgDurationSecs != 60 {
		t.Errorf("avg_duration_secs = %v, want 60", row.AvgDurationSecs)
	}
}

// TestCreateAgentRuntimeRouting pins the create-path half of JEF-237: the
// runtime_routing opt-in is accepted on POST /api/agents, defaults to "fixed"
// when absent, and rejects unknown values with a 400.
func TestCreateAgentRuntimeRouting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'create-routing-%'`,
			testWorkspaceID,
		)
	})

	base := func(name string) map[string]any {
		return map[string]any{
			"name":       name,
			"runtime_id": testRuntimeID,
			"visibility": "private",
		}
	}

	// Invalid value → 400.
	badBody := base("create-routing-invalid")
	badBody["runtime_routing"] = "smart"
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", badBody))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid runtime_routing: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Explicit "auto" → 201 and serialized back.
	autoBody := base("create-routing-auto")
	autoBody["runtime_routing"] = "auto"
	resp := testutil.Decode[AgentResponse](t, testHandler.CreateAgent,
		newRequest(http.MethodPost, "/api/agents", autoBody), http.StatusCreated)
	if resp.RuntimeRouting != "auto" {
		t.Errorf("runtime_routing = %q, want auto", resp.RuntimeRouting)
	}

	// Absent → 201, defaults to "fixed".
	defaultBody := base("create-routing-default")
	resp = testutil.Decode[AgentResponse](t, testHandler.CreateAgent,
		newRequest(http.MethodPost, "/api/agents", defaultBody), http.StatusCreated)
	if resp.RuntimeRouting != "fixed" {
		t.Errorf("runtime_routing = %q, want default fixed", resp.RuntimeRouting)
	}
}

// TestParseTaskRoutingTrace and TestRoutedTaskMatchesClaimedRuntime pin the
// claim-path helpers (JEF-237): the runtime-mismatch fence relaxes only for a
// still-auto agent whose task trace names the stamped runtime.
func TestParseTaskRoutingTrace(t *testing.T) {
	if parseTaskRoutingTrace(&db.AgentTaskQueue{}) != nil {
		t.Error("nil routing column must parse as nil (unrouted task)")
	}
	if parseTaskRoutingTrace(&db.AgentTaskQueue{Routing: []byte("{corrupt")}) != nil {
		t.Error("corrupt trace must parse as nil (fail closed)")
	}
	trace := parseTaskRoutingTrace(&db.AgentTaskQueue{Routing: []byte(
		`{"mode":"auto","chosen_runtime_id":"00000000-0000-0000-0000-00000000000b","chosen_model":"m-b"}`,
	)})
	if trace == nil || trace.ChosenRuntimeID != "00000000-0000-0000-0000-00000000000b" || trace.ChosenModel != "m-b" {
		t.Fatalf("trace = %+v", trace)
	}
}

func TestRoutedTaskMatchesClaimedRuntime(t *testing.T) {
	runtimeB := "00000000-0000-0000-0000-00000000000b"
	task := &db.AgentTaskQueue{
		RuntimeID: parseUUID(runtimeB),
		Routing:   []byte(`{"mode":"auto","chosen_runtime_id":"` + runtimeB + `","chosen_model":"m-b"}`),
	}
	autoAgent := db.Agent{RuntimeRouting: "auto"}
	fixedAgent := db.Agent{RuntimeRouting: "fixed"}

	if !routedTaskMatchesClaimedRuntime(autoAgent, task) {
		t.Error("auto agent + matching trace must pass")
	}
	if routedTaskMatchesClaimedRuntime(fixedAgent, task) {
		t.Error("fixed agent must fail even with a matching trace")
	}
	other := *task
	other.RuntimeID = parseUUID("00000000-0000-0000-0000-00000000000c")
	if routedTaskMatchesClaimedRuntime(autoAgent, &other) {
		t.Error("trace naming another runtime must fail")
	}
	untraced := *task
	untraced.Routing = nil
	if routedTaskMatchesClaimedRuntime(autoAgent, &untraced) {
		t.Error("missing trace must fail closed")
	}
}
