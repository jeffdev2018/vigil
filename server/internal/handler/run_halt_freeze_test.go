package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Halt freeze (JEF-257): setting the halt freezes in-flight runs through the
// K19 pause machinery; lifting it resumes exactly the runs the halt froze.

func putRunHalt(t *testing.T, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.PutRunHalt, testutil.WithURLParams(newRequest(http.MethodPut, "/api/run-halt", body), "id", testWorkspaceID))
}

func getRunHalt(t *testing.T) map[string]any {
	t.Helper()
	res := testutil.Call(t, testHandler.GetRunHalt, testutil.WithURLParams(newRequest(http.MethodGet, "/api/run-halt", nil), "id", testWorkspaceID)).Want(http.StatusOK)
	return res.Map()
}

func watchRunHaltEvents(t *testing.T) chan map[string]any {
	t.Helper()
	got := make(chan map[string]any, 8)
	testHandler.Bus.Subscribe(protocol.EventRunHaltChanged, func(e events.Event) {
		if p, ok := e.Payload.(map[string]any); ok {
			select {
			case got <- p:
			default:
			}
		}
	})
	return got
}

// Setting the halt freezes running runs (pause request + marker + secrets
// revoked at request time) and leaves the queue untouched.
func TestRunHaltFreezesInflightRuns(t *testing.T) {
	rememberSettings(t)
	issue, task, agent := runningAgentRun(t, "halt freeze")
	queued := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "queued"})
	dbfx.Exec(t, `INSERT INTO run_scoped_secret (id, workspace_id, task_id, agent_id, key, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, 'API_KEY', 'hash', now() + interval '1 hour')`, dbid.NewV7(), testWorkspaceID, task, agent)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM run_scoped_secret WHERE task_id = $1`, task)
	})
	got := watchRunHaltEvents(t)

	var out struct {
		Halted      bool   `json:"halted"`
		FrozenCount int    `json:"frozen_count"`
		Reason      string `json:"reason"`
	}
	putRunHalt(t, map[string]any{"halted": true, "reason": "freeze test"}).Want(http.StatusOK).JSON(&out)
	if !out.Halted || out.FrozenCount != 1 {
		t.Fatalf("halt response = %+v, want halted with frozen_count 1", out)
	}
	frozen := mustTask(t, task)
	if !frozen.PauseRequestedAt.Valid || !frozen.HaltFrozenAt.Valid || frozen.Status != "running" {
		t.Fatalf("frozen run = status %s pause_requested %v halt_frozen %v", frozen.Status, frozen.PauseRequestedAt.Valid, frozen.HaltFrozenAt.Valid)
	}
	held := mustTask(t, queued)
	if held.PauseRequestedAt.Valid || held.HaltFrozenAt.Valid || held.Status != "queued" {
		t.Fatalf("queued run was touched: %+v", held.Status)
	}
	// Secrets die at freeze-request time, not at the daemon's ack.
	var revoked bool
	var reason string
	dbfx.QueryRow(t, `SELECT revoked_at IS NOT NULL, COALESCE(revoke_reason, '') FROM run_scoped_secret WHERE task_id = $1`, task).Scan(&revoked, &reason)
	if !revoked || reason != "run_halt_freeze" {
		t.Fatalf("secret revoked=%v reason=%q, want revoked at freeze request", revoked, reason)
	}
	// Audit and the event both carry the frozen count.
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2 AND details->>'frozen' = '1'`, testWorkspaceID, AuditRunHaltChanged); n != 1 {
		t.Fatalf("audit entries with frozen=1 = %d", n)
	}
	select {
	case p := <-got:
		if p["frozen"] != 1 {
			t.Fatalf("event payload frozen = %v", p["frozen"])
		}
	default:
		t.Fatal("no run_halt:changed event")
	}
	// GET reports the live count.
	if state := getRunHalt(t); state["frozen_count"] != float64(1) {
		t.Fatalf("GET frozen_count = %v", state["frozen_count"])
	}
	// The persisted record carries no count: it is response data, not state.
	var stored service.RunHalt
	dbfx.QueryRow(t, `SELECT settings->'run_halt'::text FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&stored)
	if stored.FrozenCount != 0 || stored.ResumedCount != 0 {
		t.Fatalf("stored halt record = %+v, want zero counts", stored)
	}
	putRunHalt(t, map[string]any{"halted": false}).Want(http.StatusOK)
}

// A human pause during the halt takes ownership of the run: the marker is
// cleared, so lifting the halt leaves the run paused.
func TestRunHaltManualPauseTakesOwnership(t *testing.T) {
	rememberSettings(t)
	issue, task, agent := runningAgentRun(t, "halt own")
	got := watchRunHaltEvents(t)

	putRunHalt(t, map[string]any{"halted": true}).Want(http.StatusOK)
	if !mustTask(t, task).HaltFrozenAt.Valid {
		t.Fatal("run was not frozen by the halt")
	}
	runCall(t, testHandler.PauseRun, http.MethodPost, issue, "pause", nil).Want(http.StatusAccepted)
	owned := mustTask(t, task)
	if owned.HaltFrozenAt.Valid || !owned.PauseRequestedAt.Valid {
		t.Fatalf("manual pause = halt_frozen %v pause_requested %v, want marker cleared", owned.HaltFrozenAt.Valid, owned.PauseRequestedAt.Valid)
	}
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+task+"/paused", map[string]any{"session_id": "sess-own"}, gateHeaders(task, agent), "taskId", task).Want(http.StatusOK)

	var lift struct {
		Halted       bool `json:"halted"`
		FrozenCount  int  `json:"frozen_count"`
		ResumedCount int  `json:"resumed_count"`
	}
	putRunHalt(t, map[string]any{"halted": false}).Want(http.StatusOK).JSON(&lift)
	if lift.ResumedCount != 0 || lift.FrozenCount != 0 {
		t.Fatalf("lift = %+v, want nothing resumed", lift)
	}
	still := mustTask(t, task)
	if still.Status != "paused" || still.ResumedByTaskID.Valid {
		t.Fatalf("manually paused run after lift = status %s resumed_by %v, want untouched", still.Status, still.ResumedByTaskID.Valid)
	}
	_ = got
}

// Lifting the halt resumes the frozen-and-acked runs on their saved session,
// releases the frozen-but-unacked ones outright, and touches nothing else.
func TestRunHaltLiftResumesExactlyWhatItFroze(t *testing.T) {
	rememberSettings(t)
	// A: frozen by the halt, daemon acks with session pointers.
	_, taskA, agentA := runningAgentRun(t, "halt lift acked")
	// B: frozen by the halt, daemon offline (never acks).
	_, taskB, _ := runningAgentRun(t, "halt lift offline")
	// C: a human paused it before the halt — not the halt's to resume.
	issueC, taskC, agentC := runningAgentRun(t, "halt lift manual")
	runCall(t, testHandler.PauseRun, http.MethodPost, issueC, "pause", nil).Want(http.StatusAccepted)
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+taskC+"/paused", map[string]any{"session_id": "sess-manual"}, gateHeaders(taskC, agentC), "taskId", taskC).Want(http.StatusOK)
	got := watchRunHaltEvents(t)

	putRunHalt(t, map[string]any{"halted": true, "reason": "lift test"}).Want(http.StatusOK)
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+taskA+"/paused", map[string]any{"session_id": "sess-halt", "work_dir": "/tmp/halt", "branch_name": "feat/halt"}, gateHeaders(taskA, agentA), "taskId", taskA).Want(http.StatusOK)
	if s := mustTask(t, taskA); s.Status != "paused" || !s.HaltFrozenAt.Valid {
		t.Fatalf("acked frozen run = status %s halt_frozen %v", s.Status, s.HaltFrozenAt.Valid)
	}

	var lift struct {
		Halted       bool `json:"halted"`
		FrozenCount  int  `json:"frozen_count"`
		ResumedCount int  `json:"resumed_count"`
	}
	putRunHalt(t, map[string]any{"halted": false}).Want(http.StatusOK).JSON(&lift)
	if lift.Halted || lift.FrozenCount != 0 || lift.ResumedCount != 1 {
		t.Fatalf("lift = %+v, want resumed_count 1", lift)
	}

	// A resumed as a follow-up run pinned to the session the ack saved.
	closed := mustTask(t, taskA)
	if !closed.ResumedByTaskID.Valid || closed.HaltFrozenAt.Valid {
		t.Fatalf("resumed run = resumed_by %v halt_frozen %v", closed.ResumedByTaskID.Valid, closed.HaltFrozenAt.Valid)
	}
	child := mustTask(t, uuidToString(closed.ResumedByTaskID))
	if child.Status != "queued" || child.SessionID.String != "sess-halt" || child.WorkDir.String != "/tmp/halt" {
		t.Fatalf("resume child = status %s session %s workdir %s", child.Status, child.SessionID.String, child.WorkDir.String)
	}
	if !strings.Contains(child.HandoffNote.String, "halt") || !strings.Contains(child.HandoffNote.String, "lifted") {
		t.Fatalf("resume child note = %q", child.HandoffNote.String)
	}
	// B never acked: unfrozen, still running, and it must not pause later.
	free := mustTask(t, taskB)
	if free.Status != "running" || free.PauseRequestedAt.Valid || free.HaltFrozenAt.Valid {
		t.Fatalf("unacked run after lift = status %s pause_requested %v halt_frozen %v", free.Status, free.PauseRequestedAt.Valid, free.HaltFrozenAt.Valid)
	}
	// C was a human's pause: still paused, not resumed.
	manual := mustTask(t, taskC)
	if manual.Status != "paused" || manual.ResumedByTaskID.Valid {
		t.Fatalf("manual pause after lift = status %s resumed_by %v", manual.Status, manual.ResumedByTaskID.Valid)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2 AND details->>'resumed' = '1'`, testWorkspaceID, AuditRunHaltChanged); n != 1 {
		t.Fatalf("audit entries with resumed=1 = %d", n)
	}
	var sawLift bool
	for {
		select {
		case p := <-got:
			if p["resumed"] == 1 {
				sawLift = true
			}
		default:
			if !sawLift {
				t.Fatal("no run_halt:changed event with resumed=1")
			}
			return
		}
	}
}

// The ack-path revoke stays idempotent: a secret already revoked at freeze
// time is not revoked again when the daemon acks.
func TestRunHaltFreezeAckRevokeIsIdempotent(t *testing.T) {
	rememberSettings(t)
	_, task, agent := runningAgentRun(t, "halt ack")
	dbfx.Exec(t, `INSERT INTO run_scoped_secret (id, workspace_id, task_id, agent_id, key, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, 'API_KEY', 'hash', now() + interval '1 hour')`, dbid.NewV7(), testWorkspaceID, task, agent)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM run_scoped_secret WHERE task_id = $1`, task)
	})
	putRunHalt(t, map[string]any{"halted": true}).Want(http.StatusOK)
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+task+"/paused", map[string]any{"session_id": "sess-ack"}, gateHeaders(task, agent), "taskId", task).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2 AND details->>'reason' = 'run_paused' AND entity_id = $3`, testWorkspaceID, AuditRunSecretRevoked, task); n != 0 {
		t.Fatalf("ack re-revoked an already revoked secret: %d audit entries", n)
	}
	var reason string
	dbfx.QueryRow(t, `SELECT revoke_reason FROM run_scoped_secret WHERE task_id = $1`, task).Scan(&reason)
	if reason != "run_halt_freeze" {
		t.Fatalf("revoke reason = %q, want the freeze-time reason kept", reason)
	}
	putRunHalt(t, map[string]any{"halted": false}).Want(http.StatusOK)
}

// A resume child continues the PAUSED TASK's session, so it belongs to the
// task's agent — the issue having no assignee must not make the run
// unresumable.
func TestRunHaltLiftResumesUnassignedIssue(t *testing.T) {
	rememberSettings(t)
	runtimeID := handlerTestRuntimeID(t)
	agent := dbfx.Agent(t, "halt unassigned agent", runtimeID)
	issue := dbfx.Issue(t, "halt unassigned issue")
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": runtimeID, "issue_id": issue, "status": "running", "started_at": testutil.Raw("now()")})

	putRunHalt(t, map[string]any{"halted": true}).Want(http.StatusOK)
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+task+"/paused", map[string]any{"session_id": "sess-unassigned", "work_dir": "/tmp/u"}, gateHeaders(task, agent), "taskId", task).Want(http.StatusOK)

	var lift struct {
		ResumedCount int `json:"resumed_count"`
	}
	putRunHalt(t, map[string]any{"halted": false}).Want(http.StatusOK).JSON(&lift)
	if lift.ResumedCount != 1 {
		t.Fatalf("lift resumed_count = %d, want 1 for the unassigned issue's run", lift.ResumedCount)
	}
	closed := mustTask(t, task)
	if !closed.ResumedByTaskID.Valid {
		t.Fatal("frozen run was not resumed")
	}
	child := mustTask(t, uuidToString(closed.ResumedByTaskID))
	if uuidToString(child.AgentID) != agent || child.SessionID.String != "sess-unassigned" || uuidToString(child.RuntimeID) != runtimeID {
		t.Fatalf("child agent=%s session=%s runtime=%s, want the task's agent/session/runtime", uuidToString(child.AgentID), child.SessionID.String, uuidToString(child.RuntimeID))
	}
}

// The issue being reassigned mid-halt must not hand the saved session to the
// new assignee: the child runs on the paused task's agent and is pinned to
// the runtime where the session lives.
func TestRunHaltLiftResumesOnPausedTasksAgent(t *testing.T) {
	rememberSettings(t)
	runtimeA := dbfx.Runtime(t, "halt reassign runtime a", testutil.Cols{"provider": "claude"})
	runtimeB := dbfx.Runtime(t, "halt reassign runtime b", testutil.Cols{"provider": "claude"})
	agentA := dbfx.Agent(t, "halt reassign agent a", runtimeA)
	agentB := dbfx.Agent(t, "halt reassign agent b", runtimeB)
	// The task ran on A; the issue is NOW assigned to B.
	issue := dbfx.Issue(t, "halt reassign issue", testutil.Cols{"assignee_type": "agent", "assignee_id": agentB})
	task := dbfx.Task(t, agentA, testutil.Cols{"runtime_id": runtimeA, "issue_id": issue, "status": "running", "started_at": testutil.Raw("now()")})

	putRunHalt(t, map[string]any{"halted": true}).Want(http.StatusOK)
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+task+"/paused", map[string]any{"session_id": "sess-a", "work_dir": "/tmp/a"}, gateHeaders(task, agentA), "taskId", task).Want(http.StatusOK)

	var lift struct {
		ResumedCount int `json:"resumed_count"`
	}
	putRunHalt(t, map[string]any{"halted": false}).Want(http.StatusOK).JSON(&lift)
	if lift.ResumedCount != 1 {
		t.Fatalf("lift resumed_count = %d, want 1", lift.ResumedCount)
	}
	child := mustTask(t, uuidToString(mustTask(t, task).ResumedByTaskID))
	if uuidToString(child.AgentID) != agentA {
		t.Fatalf("child agent = %s, want the paused task's agent %s (not the new assignee %s)", uuidToString(child.AgentID), agentA, agentB)
	}
	if child.SessionID.String != "sess-a" || uuidToString(child.RuntimeID) != runtimeA || !child.RuntimePinned {
		t.Fatalf("child session=%s runtime=%s pinned=%v, want sess-a on the original runtime, pinned", child.SessionID.String, uuidToString(child.RuntimeID), child.RuntimePinned)
	}
}
