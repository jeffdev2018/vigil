package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The halt is the one switch that stops everything, so what it stops and what
// it deliberately does not are both pinned here.
func TestRunHaltStopsDispatchAndGatedActions(t *testing.T) {
	var previous []byte
	testPool.QueryRow(context.Background(), `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, testWorkspaceID)
	})
	halt := func(t *testing.T, body map[string]any, want int) {
		t.Helper()
		testutil.Call(t, testHandler.PutRunHalt, testutil.WithURLParams(newRequest(http.MethodPut, "/api/run-halt", body), "id", testWorkspaceID)).Want(want)
	}

	runtimeID := dbfx.Runtime(t, "halt runtime "+uuid.NewString()[:6], testutil.Cols{"provider": "claude"})
	agentID := dbfx.Agent(t, "halt agent "+uuid.NewString()[:6], runtimeID)
	issueID := dbfx.Issue(t, "halt issue "+uuid.NewString()[:6], testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})

	claim := func(t *testing.T) string {
		t.Helper()
		var out struct {
			Task *struct {
				ID string `json:"id"`
			} `json:"task"`
		}
		testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(
			newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "halt-daemon"),
			"runtimeId", runtimeID)).Want(http.StatusOK).JSON(&out)
		if out.Task == nil {
			return ""
		}
		return out.Task.ID
	}

	halt(t, map[string]any{"halted": true, "reason": "an agent opened 40 PRs"}, http.StatusOK)
	if got := claim(t); got != "" {
		t.Fatalf("claimed %q while halted: a held workspace must dispatch nothing", got)
	}
	// Held, not lost: the task is still queued and still claimable later.
	if status := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE id = $1 AND status = 'queued'`, taskID); status != 1 {
		t.Fatal("a halt holds the queue; it must not fail or consume the task")
	}

	// A run already in flight keeps its tools — those were resolved at claim
	// and cannot be taken back — but loses every action that asks the server
	// first, which is every action the product treats as consequential.
	// On its own runtime: a runtime already holding a running task claims no
	// other one, and this assertion is about the gate, not about dispatch.
	flightRuntime := dbfx.Runtime(t, "halt inflight runtime "+uuid.NewString()[:6], testutil.Cols{"provider": "claude"})
	flightAgent := dbfx.Agent(t, "halt inflight agent "+uuid.NewString()[:6], flightRuntime)
	flightIssue := dbfx.Issue(t, "halt inflight issue "+uuid.NewString()[:6], testutil.Cols{"assignee_type": "agent", "assignee_id": flightAgent})
	running := dbfx.Task(t, flightAgent, testutil.Cols{"runtime_id": flightRuntime, "issue_id": flightIssue, "status": "running"})
	gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+running+"/gates",
		map[string]any{"gate_type": "git_push", "summary": "push to main"},
		gateHeaders(running, flightAgent), "taskId", running).Want(http.StatusUnprocessableEntity)

	// Every member can see who stopped what and why.
	var state struct {
		Halted   bool   `json:"halted"`
		Reason   string `json:"reason"`
		HaltedBy string `json:"halted_by"`
		HaltedAt string `json:"halted_at"`
	}
	testutil.Call(t, testHandler.GetRunHalt, testutil.WithURLParams(newRequest(http.MethodGet, "/api/run-halt", nil), "id", testWorkspaceID)).Want(http.StatusOK).JSON(&state)
	if !state.Halted || state.Reason != "an agent opened 40 PRs" || state.HaltedBy != testUserID || state.HaltedAt == "" {
		t.Fatalf("halt state = %+v, want the reason, the author and the time", state)
	}

	// Lifting clears the record rather than leaving a stale author behind.
	// Decoded into a fresh value on purpose: the resumed record omits the
	// cleared keys, so reusing the struct above would keep showing the old
	// author and read as a bug that is not there.
	halt(t, map[string]any{"halted": false}, http.StatusOK)
	var resumed struct {
		Halted   bool   `json:"halted"`
		Reason   string `json:"reason"`
		HaltedBy string `json:"halted_by"`
	}
	testutil.Call(t, testHandler.GetRunHalt, testutil.WithURLParams(newRequest(http.MethodGet, "/api/run-halt", nil), "id", testWorkspaceID)).Want(http.StatusOK).JSON(&resumed)
	if resumed.Halted || resumed.Reason != "" || resumed.HaltedBy != "" {
		t.Fatalf("after resume = %+v, want a cleared record", resumed)
	}
	if got := claim(t); got != taskID {
		t.Fatalf("claimed %q after resume, want the held task %q back", got, taskID)
	}
}

// A halt is a decision, so it is owner/admin only and bounded.
func TestRunHaltPermissionsAndValidation(t *testing.T) {
	var previous []byte
	testPool.QueryRow(context.Background(), `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, testWorkspaceID)
	})

	member := dbfx.User(t, "halt member", "halt-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	testutil.Call(t, testHandler.PutRunHalt, testutil.WithURLParams(
		newRequestAs(member, http.MethodPut, "/api/run-halt", map[string]any{"halted": true}), "id", testWorkspaceID)).Want(http.StatusForbidden)
	// …but a member can read it: someone whose run was refused has to be able
	// to find out by whom.
	testutil.Call(t, testHandler.GetRunHalt, testutil.WithURLParams(
		newRequestAs(member, http.MethodGet, "/api/run-halt", nil), "id", testWorkspaceID)).Want(http.StatusOK)

	long := make([]byte, 0, 600)
	for i := 0; i < 600; i++ {
		long = append(long, 'x')
	}
	testutil.Call(t, testHandler.PutRunHalt, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/run-halt", map[string]any{"halted": true, "reason": string(long)}), "id", testWorkspaceID)).Want(http.StatusBadRequest)
}
