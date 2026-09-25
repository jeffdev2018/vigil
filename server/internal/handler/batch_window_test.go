package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Off-peak batch lane (K45). The window arithmetic is covered once, in
// service/batch_window_test.go. These tests cover the endpoint contract, the
// autopilot flag that opts work into the lane, and the one behaviour that only
// exists end to end: a batch task waits behind a sync one even when it was
// queued first.

// TestBatchWindowEndpoint: the window round-trips, is admin-only to write, and
// refuses a window it cannot act on rather than storing one.
func TestBatchWindowEndpoint(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rememberSettings(t)

	var got map[string]any
	testutil.Call(t, testHandler.GetBatchWindow, newRequest(http.MethodGet, "/api/batch-window", nil)).
		Want(http.StatusOK).JSON(&got)
	if got["enabled"] != false {
		t.Fatalf("a workspace with no window must report disabled, got %v", got["enabled"])
	}
	if got["timezone"] != "UTC" {
		t.Fatalf("timezone defaults to UTC, got %v", got["timezone"])
	}

	testutil.Call(t, testHandler.PutBatchWindow, newRequest(http.MethodPut, "/api/batch-window", map[string]any{
		"enabled":          true,
		"start_local_time": " 22:00 ",
		"end_local_time":   "06:00",
		"timezone":         "Europe/Paris",
	})).Want(http.StatusOK).JSON(&got)
	if got["enabled"] != true || got["start_local_time"] != "22:00" || got["end_local_time"] != "06:00" || got["timezone"] != "Europe/Paris" {
		t.Fatalf("stored window = %v, want the normalized 22:00→06:00 Europe/Paris", got)
	}

	var reread map[string]any
	testutil.Call(t, testHandler.GetBatchWindow, newRequest(http.MethodGet, "/api/batch-window", nil)).
		Want(http.StatusOK).JSON(&reread)
	if reread["start_local_time"] != "22:00" || reread["timezone"] != "Europe/Paris" {
		t.Fatalf("the stored window must survive the round trip, got %v", reread)
	}

	// Rejections. Each of these would otherwise be stored as a window the
	// scheduler silently ignores, which reads to the user as "the feature is
	// broken" rather than "that input was refused".
	for name, body := range map[string]map[string]any{
		"unknown timezone":  {"enabled": true, "start_local_time": "22:00", "end_local_time": "06:00", "timezone": "Mars/Olympus"},
		"equal bounds":      {"enabled": true, "start_local_time": "03:00", "end_local_time": "03:00", "timezone": "UTC"},
		"missing start":     {"enabled": true, "end_local_time": "06:00", "timezone": "UTC"},
		"hour out of range": {"enabled": true, "start_local_time": "24:00", "end_local_time": "06:00", "timezone": "UTC"},
	} {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.PutBatchWindow, newRequest(http.MethodPut, "/api/batch-window", body)).
				Want(http.StatusBadRequest)
		})
	}
	testutil.Call(t, testHandler.PutBatchWindow, newRequest(http.MethodPut, "/api/batch-window", "not an object")).
		Want(http.StatusBadRequest)

	// Disabling must not be blocked by the times it stops reading — otherwise
	// a workspace that mistyped a window could not turn the feature off.
	testutil.Call(t, testHandler.PutBatchWindow, newRequest(http.MethodPut, "/api/batch-window", map[string]any{
		"enabled": false,
	})).Want(http.StatusOK).JSON(&got)
	if got["enabled"] != false {
		t.Fatalf("enabled = %v after disabling, want false", got["enabled"])
	}
	testutil.Call(t, testHandler.GetBatchWindow, newRequest(http.MethodGet, "/api/batch-window", nil)).
		Want(http.StatusOK).JSON(&reread)
	if reread["enabled"] != false {
		t.Fatalf("a disabled window must stay disabled after a re-read, got %v", reread)
	}

	// A plain member reads the window but cannot change it: it decides when a
	// whole workspace's non-urgent work is allowed to wait.
	plain := dbfx.User(t, "batch window member", "batch-window-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, plain, "member")
	testutil.Call(t, testHandler.GetBatchWindow, newRequestAs(plain, http.MethodGet, "/api/batch-window", nil)).
		Want(http.StatusOK)
	testutil.Call(t, testHandler.PutBatchWindow, newRequestAs(plain, http.MethodPut, "/api/batch-window", map[string]any{
		"enabled": true, "start_local_time": "01:00", "end_local_time": "05:00", "timezone": "UTC",
	})).Want(http.StatusForbidden)
}

// TestClaimServesSyncLaneBeforeBatchLane is the whole point of the lane: an
// off-peak task queued FIRST must still wait behind a synchronous task queued
// after it, because "cheaper, later" must never mean "and it delays real work".
// Without the lane term in the claim's ORDER BY, created_at alone would hand
// the batch task out first.
func TestClaimServesSyncLaneBeforeBatchLane(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := dbfx.Runtime(t, "batch lane rt "+uuid.NewString()[:8], testutil.Cols{
		"daemon_id": "batch-lane-daemon-" + uuid.NewString()[:8],
	})
	agentID := dbfx.Agent(t, "batch lane agent "+uuid.NewString()[:8], runtimeID)

	batchIssue := dbfx.Issue(t, "off-peak work", testutil.Cols{"number": 88451})
	syncIssue := dbfx.Issue(t, "urgent work", testutil.Cols{"number": 88452})

	// Queued first, and with the HIGHER priority, so neither of the two terms
	// the lane sorts ahead of could produce this ordering on its own.
	batchTaskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":    runtimeID,
		"issue_id":      batchIssue,
		"priority":      10,
		"dispatch_lane": "batch",
		"created_at":    testutil.Raw("now() - interval '1 hour'"),
	})
	syncTaskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   syncIssue,
		"priority":   0,
	})

	var daemonID string
	dbfx.QueryRow(t, `SELECT daemon_id FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&daemonID)
	claim := func() string {
		t.Helper()
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, daemonID)
		req = withURLParam(req, "runtimeId", runtimeID)
		var resp struct {
			Task *struct {
				ID           string `json:"id"`
				DispatchLane string `json:"dispatch_lane"`
			} `json:"task"`
		}
		testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&resp)
		if resp.Task == nil {
			t.Fatal("expected a task in the claim response")
		}
		if resp.Task.ID == batchTaskID && resp.Task.DispatchLane != "batch" {
			t.Fatalf("the batch task claimed as lane %q; the claim must report the lane it was stamped with", resp.Task.DispatchLane)
		}
		return resp.Task.ID
	}

	if got := claim(); got != syncTaskID {
		t.Fatalf("first claim returned %s, want the sync task %s — an off-peak task queued earlier and at a higher priority must still yield", got, syncTaskID)
	}

	// Retire the sync run so the runtime is free again: the point of the
	// second claim is that yielding is a delay, not a starvation.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, syncTaskID)
	if got := claim(); got != batchTaskID {
		t.Fatalf("second claim returned %s, want the batch task %s — the off-peak task must still be served once nothing sync is waiting", got, batchTaskID)
	}
}

// TestAutopilotBatchEligibleRoundTrips: the opt-in survives create, read and
// update, and an update that omits it leaves it alone.
func TestAutopilotBatchEligibleRoundTrips(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "batch-eligible-ap-"+uuid.NewString()[:8], nil)

	var created AutopilotResponse
	testutil.Call(t, testHandler.CreateAutopilot, newRequest(http.MethodPost, "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "nightly sweep",
		"assignee_type":  "agent",
		"assignee_id":    agentID,
		"execution_mode": "run_only",
		"batch_eligible": true,
	})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM autopilot WHERE id = $1`, created.ID) })
	if !created.BatchEligible {
		t.Fatalf("batch_eligible = false on create, want true")
	}

	// GET wraps the autopilot alongside its triggers and collaborators.
	var detail struct {
		Autopilot AutopilotResponse `json:"autopilot"`
	}
	testutil.Call(t, testHandler.GetAutopilot,
		withURLParam(newRequest(http.MethodGet, "/api/autopilots/"+created.ID, nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&detail)
	if !detail.Autopilot.BatchEligible {
		t.Fatalf("batch_eligible = false on read, want true")
	}

	var got AutopilotResponse

	// An update that says nothing about the flag must not clear it — every
	// other optional field on this endpoint round-trips unchanged when omitted.
	testutil.Call(t, testHandler.UpdateAutopilot,
		withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+created.ID, map[string]any{"title": "renamed"}), "id", created.ID)).
		Want(http.StatusOK).JSON(&got)
	if !got.BatchEligible {
		t.Fatalf("an update that omitted batch_eligible cleared it")
	}

	testutil.Call(t, testHandler.UpdateAutopilot,
		withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+created.ID, map[string]any{"batch_eligible": false}), "id", created.ID)).
		Want(http.StatusOK).JSON(&got)
	if got.BatchEligible {
		t.Fatalf("batch_eligible = true after opting out, want false")
	}
}

// TestAutopilotRunReportsDispatchLane: a run row carries the lane of the task it
// linked, so the history says "this one waited for off-peak" instead of leaving
// a late start looking like a stall. The lane lives on the task, not the run, so
// this is the plumbing that joins them.
func TestAutopilotRunReportsDispatchLane(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "run-lane-ap-"+uuid.NewString()[:8], nil)

	var created AutopilotResponse
	testutil.Call(t, testHandler.CreateAutopilot, newRequest(http.MethodPost, "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "lane reporting",
		"assignee_type":  "agent",
		"assignee_id":    agentID,
		"execution_mode": "run_only",
		"batch_eligible": true,
	})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM autopilot WHERE id = $1`, created.ID) })

	runtimeID := dbfx.Runtime(t, "run lane rt "+uuid.NewString()[:8])
	batchTask := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "dispatch_lane": "batch"})
	syncTask := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	for _, taskID := range []string{batchTask, syncTask} {
		dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": created.ID,
			"source":       "schedule",
			"status":       "running",
			"task_id":      taskID,
		})
	}

	var out struct {
		Runs []AutopilotRunResponse `json:"runs"`
	}
	testutil.Call(t, testHandler.ListAutopilotRuns,
		withURLParam(newRequest(http.MethodGet, "/api/autopilots/"+created.ID+"/runs?workspace_id="+testWorkspaceID, nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&out)

	lanes := map[string]string{}
	for _, run := range out.Runs {
		if run.TaskID != nil {
			lanes[*run.TaskID] = run.DispatchLane
		}
	}
	if lanes[batchTask] != "batch" {
		t.Fatalf("the off-peak run reported lane %q, want batch", lanes[batchTask])
	}
	// The ordinary run must not be labelled: a badge on every row says nothing.
	if lanes[syncTask] != "sync" {
		t.Fatalf("the ordinary run reported lane %q, want sync", lanes[syncTask])
	}
}

// TestSettingsPutsDoNotEraseEachOther: every settings PUT used to read the
// whole `workspace.settings` blob, change one key and write it all back, so
// two PUTs on different keys could erase each other. They now merge one key
// server-side; a second key written afterwards must find the first intact.
func TestSettingsPutsDoNotEraseEachOther(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rememberSettings(t)

	testutil.Call(t, testHandler.PutBatchWindow, newRequest(http.MethodPut, "/api/batch-window", map[string]any{
		"enabled": true, "start_local_time": "22:00", "end_local_time": "06:00", "timezone": "UTC",
	})).Want(http.StatusOK)
	testutil.Call(t, testHandler.PutWorkflowPolicySettings, newRequest(http.MethodPut, "/api/workflow-policy", map[string]any{"mode": "auto"})).Want(http.StatusOK)

	var window map[string]any
	testutil.Call(t, testHandler.GetBatchWindow, newRequest(http.MethodGet, "/api/batch-window", nil)).Want(http.StatusOK).JSON(&window)
	if window["enabled"] != true || window["start_local_time"] != "22:00" {
		t.Fatalf("the batch window was erased by the workflow policy write: %v", window)
	}
	var policy map[string]any
	testutil.Call(t, testHandler.GetWorkflowPolicySettings, newRequest(http.MethodGet, "/api/workflow-policy", nil)).Want(http.StatusOK).JSON(&policy)
	if policy["mode"] != "auto" {
		t.Fatalf("the workflow policy did not survive: %v", policy)
	}
}
