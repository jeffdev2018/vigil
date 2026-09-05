package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Tests for GET /api/runtimes/routing-stats (JEF-237).
//
// The happy path — success rate, cost/duration averages, provider
// lower-casing and the runtime-name join — is pinned by
// TestGetRuntimeRoutingStats in agent_runtime_routing_test.go. This file
// covers the boundaries around it: access, workspace isolation, the empty
// answer, which runs the underlying GetRoutingStats query is allowed to
// count, and the null-vs-zero average distinction.

// routingStatsRequest builds the endpoint's request for an arbitrary
// user/workspace pair, which is what the isolation and access cases need and
// newRequest (hard-wired to the suite's user and workspace) cannot express.
func routingStatsRequest(userID, workspaceID string) *http.Request {
	req := testutil.JSONRequest(http.MethodGet, "/api/runtimes/routing-stats", nil)
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	return req
}

// findRoutingStatsRow returns the row for one (model, task_class) pair, or nil.
// Model names in these tests are unique per case, so this is enough to pick a
// seeded row out of a workspace other tests also write to.
func findRoutingStatsRow(resp RuntimeRoutingStatsResponse, model, taskClass string) *RuntimeRoutingStatsRow {
	for i := range resp.Rows {
		if resp.Rows[i].Model == model && resp.Rows[i].TaskClass == taskClass {
			return &resp.Rows[i]
		}
	}
	return nil
}

// seedRoutingRun queues one terminal run with a usage row attached, which is
// the minimum GetRoutingStats counts: a completed/failed task with a
// completed_at and at least one task_usage row to attribute it to a model.
// A nil usage map means "no task_usage row at all" — the unattributable run
// the query is expected to drop; pass an empty map to take the defaults.
func seedRoutingRun(t *testing.T, agentID, runtimeID, model, taskClass string, task, usage testutil.Cols) string {
	t.Helper()

	taskID := dbfx.Task(t, agentID, mergeCols(testutil.Cols{
		"runtime_id":   runtimeID,
		"status":       "completed",
		"task_class":   taskClass,
		"started_at":   testutil.Raw("now() - interval '2 minutes'"),
		"completed_at": testutil.Raw("now() - interval '1 minute'"),
	}, task))
	if usage != nil {
		dbfx.Insert(t, "task_usage", mergeCols(testutil.Cols{
			"task_id":        taskID,
			"provider":       "openai",
			"model":          model,
			"cost_usd_ticks": 1_000_000_000, // $0.10
		}, usage))
	}
	return taskID
}

func mergeCols(base, over testutil.Cols) testutil.Cols {
	out := make(testutil.Cols, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// TestRuntimeRoutingStatsAccess pins what the endpoint answers when the
// caller cannot be resolved to a member of the requested workspace. There is
// no query-parameter surface here: the window is the fixed 90-day constant,
// so the only inputs are the user and workspace identifiers.
func TestRuntimeRoutingStatsAccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	suffix := time.Now().UnixNano()
	otherUserID := dbfx.User(t, "Routing Stats Outsider",
		fmt.Sprintf("routing-stats-outsider-%d@multica.ai", suffix))

	cases := []struct {
		name        string
		userID      string
		workspaceID string
		want        int
	}{
		{"no workspace identifier", testUserID, "", http.StatusBadRequest},
		{"no user identifier", "", testWorkspaceID, http.StatusUnauthorized},
		{"non-member user", otherUserID, testWorkspaceID, http.StatusNotFound},
		{"unknown workspace", testUserID, "00000000-0000-0000-0000-0000000000ff", http.StatusNotFound},
		// A malformed identifier must be rejected, not panic in parseUUID.
		{"malformed workspace identifier", testUserID, "not-a-uuid", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Call(t, testHandler.GetRuntimeRoutingStats,
				routingStatsRequest(tc.userID, tc.workspaceID)).Want(tc.want)
		})
	}
}

// TestRuntimeRoutingStatsEmptyWorkspace: a member of a workspace that has
// never run anything gets 200 and an empty JSON array, not 404 and not a
// null `rows` the client would have to guard.
func TestRuntimeRoutingStatsEmptyWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	suffix := time.Now().UnixNano()
	userID := dbfx.User(t, "Routing Stats Empty",
		fmt.Sprintf("routing-stats-empty-%d@multica.ai", suffix))
	workspaceID := dbfx.Workspace(t, "Routing Stats Empty",
		fmt.Sprintf("routing-stats-empty-%d", suffix))
	dbfx.Member(t, workspaceID, userID, "owner")

	resp := testutil.Call(t, testHandler.GetRuntimeRoutingStats,
		routingStatsRequest(userID, workspaceID)).Want(http.StatusOK)

	var body RuntimeRoutingStatsResponse
	resp.JSON(&body)
	if body.WindowDays != routingStatsWindowDays {
		t.Errorf("window_days = %d, want %d", body.WindowDays, routingStatsWindowDays)
	}
	if len(body.Rows) != 0 {
		t.Errorf("rows = %+v, want none for a workspace with no runs", body.Rows)
	}
	// Guard the wire shape, not just the decoded value: `null` would decode
	// into a nil slice here and break clients that iterate it directly.
	if !strings.Contains(resp.Text(), `"rows":[]`) {
		t.Errorf("body = %s, want an empty rows array", resp.Text())
	}
}

// TestRuntimeRoutingStatsWorkspaceIsolation: runs recorded in another
// workspace never appear in this workspace's stats, in either direction.
func TestRuntimeRoutingStatsWorkspaceIsolation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	suffix := time.Now().UnixNano()
	otherUserID := dbfx.User(t, "Routing Stats Neighbour",
		fmt.Sprintf("routing-stats-neighbour-%d@multica.ai", suffix))
	otherWorkspaceID := dbfx.Workspace(t, "Routing Stats Neighbour",
		fmt.Sprintf("routing-stats-neighbour-%d", suffix))
	dbfx.Member(t, otherWorkspaceID, otherUserID, "owner")

	otherRuntimeID := dbfx.Runtime(t, "Neighbour Runtime", testutil.Cols{
		"workspace_id": otherWorkspaceID,
		"owner_id":     otherUserID,
	})
	otherAgentID := dbfx.Agent(t, "neighbour-agent", otherRuntimeID, testutil.Cols{
		"workspace_id": otherWorkspaceID,
		"owner_id":     otherUserID,
	})
	const neighbourModel = "routing-stats-neighbour-model"
	seedRoutingRun(t, otherAgentID, otherRuntimeID, neighbourModel, "bugfix", nil, testutil.Cols{})

	// The neighbour sees its own run...
	neighbour := testutil.Decode[RuntimeRoutingStatsResponse](t, testHandler.GetRuntimeRoutingStats,
		routingStatsRequest(otherUserID, otherWorkspaceID), http.StatusOK)
	if findRoutingStatsRow(neighbour, neighbourModel, "bugfix") == nil {
		t.Fatalf("neighbour workspace is missing its own run: %+v", neighbour.Rows)
	}

	// ...and the suite's workspace does not.
	mine := testutil.Decode[RuntimeRoutingStatsResponse](t, testHandler.GetRuntimeRoutingStats,
		newRequest(http.MethodGet, "/api/runtimes/routing-stats", nil), http.StatusOK)
	if row := findRoutingStatsRow(mine, neighbourModel, "bugfix"); row != nil {
		t.Errorf("another workspace's stats leaked in: %+v", row)
	}
	for _, row := range mine.Rows {
		if row.RuntimeID == otherRuntimeID {
			t.Errorf("another workspace's runtime %s leaked in: %+v", otherRuntimeID, row)
		}
	}
}

// TestRuntimeRoutingStatsExcludesNonQualifyingRuns: a run only counts when it
// is terminal, has a completed_at inside the 90-day window, and carries usage
// that attributes it to a model. Each exclusion gets its own model name so a
// failure names the rule that broke.
func TestRuntimeRoutingStatsExcludesNonQualifyingRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "routing-stats-exclusions-agent", nil)

	excluded := []struct {
		name  string
		model string
		task  testutil.Cols
		usage testutil.Cols
	}{
		{
			name:  "still running",
			model: "routing-stats-running-model",
			task:  testutil.Cols{"status": "running", "completed_at": nil},
		},
		{
			name:  "cancelled",
			model: "routing-stats-cancelled-model",
			task:  testutil.Cols{"status": "cancelled"},
		},
		{
			name:  "terminal without completed_at",
			model: "routing-stats-no-completed-at-model",
			task:  testutil.Cols{"completed_at": nil},
		},
		{
			name:  "completed before the window",
			model: "routing-stats-stale-model",
			task: testutil.Cols{
				"started_at":   testutil.Raw("now() - interval '91 days'"),
				"completed_at": testutil.Raw("now() - interval '91 days'"),
			},
		},
	}
	for _, tc := range excluded {
		seedRoutingRun(t, agentID, runtimeID, tc.model, "general", tc.task, testutil.Cols{})
	}
	// A completed, in-window run with no task_usage row at all cannot be
	// attributed to a provider/model, so it is not counted either. It has no
	// model name to look for — assert on the sample count of the class it
	// would otherwise inflate.
	const countedModel = "routing-stats-counted-model"
	seedRoutingRun(t, agentID, runtimeID, countedModel, "unattributed-class", nil, nil)
	seedRoutingRun(t, agentID, runtimeID, countedModel, "unattributed-class", nil, testutil.Cols{})

	resp := testutil.Decode[RuntimeRoutingStatsResponse](t, testHandler.GetRuntimeRoutingStats,
		newRequest(http.MethodGet, "/api/runtimes/routing-stats", nil), http.StatusOK)

	for _, tc := range excluded {
		if row := findRoutingStatsRow(resp, tc.model, "general"); row != nil {
			t.Errorf("%s: run was counted: %+v", tc.name, row)
		}
	}
	row := findRoutingStatsRow(resp, countedModel, "unattributed-class")
	if row == nil {
		t.Fatalf("the attributed run is missing: %+v", resp.Rows)
	}
	if row.Samples != 1 {
		t.Errorf("samples = %d, want 1 — the run without task_usage must not count", row.Samples)
	}
}

// TestRuntimeRoutingStatsSegmentsByTaskClass: the same (runtime, provider,
// model) splits into one row per task_class, which is what makes the stats
// usable for per-class routing decisions.
func TestRuntimeRoutingStatsSegmentsByTaskClass(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "routing-stats-classes-agent", nil)
	const model = "routing-stats-classes-model"

	seedRoutingRun(t, agentID, runtimeID, model, "bugfix", nil, testutil.Cols{})
	seedRoutingRun(t, agentID, runtimeID, model, "review", nil, testutil.Cols{})
	seedRoutingRun(t, agentID, runtimeID, model, "review", testutil.Cols{"status": "failed"},
		testutil.Cols{})

	resp := testutil.Decode[RuntimeRoutingStatsResponse](t, testHandler.GetRuntimeRoutingStats,
		newRequest(http.MethodGet, "/api/runtimes/routing-stats", nil), http.StatusOK)

	bugfix := findRoutingStatsRow(resp, model, "bugfix")
	review := findRoutingStatsRow(resp, model, "review")
	if bugfix == nil || review == nil {
		t.Fatalf("want one row per task_class, got %+v", resp.Rows)
	}
	if bugfix.Samples != 1 || bugfix.SuccessRate != 1 {
		t.Errorf("bugfix = %d samples / %f success, want 1 / 1", bugfix.Samples, bugfix.SuccessRate)
	}
	if review.Samples != 2 || review.SuccessRate != 0.5 {
		t.Errorf("review = %d samples / %f success, want 2 / 0.5", review.Samples, review.SuccessRate)
	}
	if bugfix.RuntimeID != runtimeID || review.RuntimeID != runtimeID {
		t.Errorf("runtime_id = %q / %q, want %q", bugfix.RuntimeID, review.RuntimeID, runtimeID)
	}
}

// TestRuntimeRoutingStatsNullAverages: a run with no priced usage and no
// start time reports null averages, not 0.0 — a router that read those as
// "free and instant" would always pick the runtime it knows least about.
func TestRuntimeRoutingStatsNullAverages(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "routing-stats-nulls-agent", nil)
	const model = "routing-stats-nulls-model"

	seedRoutingRun(t, agentID, runtimeID, model, "general",
		testutil.Cols{"started_at": nil},
		testutil.Cols{"cost_usd_ticks": nil})

	resp := testutil.Call(t, testHandler.GetRuntimeRoutingStats,
		newRequest(http.MethodGet, "/api/runtimes/routing-stats", nil)).Want(http.StatusOK)

	var body RuntimeRoutingStatsResponse
	resp.JSON(&body)
	row := findRoutingStatsRow(body, model, "general")
	if row == nil {
		t.Fatalf("the unpriced run is missing: %+v", body.Rows)
	}
	if row.Samples != 1 || row.SuccessRate != 1 {
		t.Errorf("samples/success = %d / %f, want 1 / 1", row.Samples, row.SuccessRate)
	}
	if row.AvgCostUSD != nil {
		t.Errorf("avg_cost_usd = %v, want null for a run with no provider cost", *row.AvgCostUSD)
	}
	if row.AvgDurationSecs != nil {
		t.Errorf("avg_duration_secs = %v, want null for a run that never started", *row.AvgDurationSecs)
	}
	if !strings.Contains(resp.Text(), `"avg_cost_usd":null`) {
		t.Errorf("body = %s, want a null avg_cost_usd on the wire", resp.Text())
	}
}

// TestRuntimeRoutingStatsMultiModelRunCountsOnce pins the attribution rule: a
// run that consumed two models is one sample, credited to the dominant
// (most expensive) model with the run's total cost, not one sample per model.
func TestRuntimeRoutingStatsMultiModelRunCountsOnce(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "routing-stats-multimodel-agent", nil)
	const major = "routing-stats-multimodel-major"
	const minor = "routing-stats-multimodel-minor"

	taskID := seedRoutingRun(t, agentID, runtimeID, major, "general",
		testutil.Cols{}, testutil.Cols{"cost_usd_ticks": 3_000_000_000})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":        taskID,
		"provider":       "openai",
		"model":          minor,
		"cost_usd_ticks": 1_000_000_000,
	})

	var body RuntimeRoutingStatsResponse
	testutil.Call(t, testHandler.GetRuntimeRoutingStats,
		newRequest(http.MethodGet, "/api/runtimes/routing-stats", nil)).Want(http.StatusOK).JSON(&body)

	if row := findRoutingStatsRow(body, minor, "general"); row != nil {
		t.Errorf("the minor model got its own row %+v; the run must be attributed once", *row)
	}
	row := findRoutingStatsRow(body, major, "general")
	if row == nil {
		t.Fatalf("the dominant model row is missing: %+v", body.Rows)
	}
	if row.Samples != 1 {
		t.Errorf("samples = %d, want 1 for one two-model run", row.Samples)
	}
	if row.AvgCostUSD == nil || *row.AvgCostUSD != 0.4 {
		t.Errorf("avg_cost_usd = %v, want 0.4 (the run's total across both models)", row.AvgCostUSD)
	}
}
