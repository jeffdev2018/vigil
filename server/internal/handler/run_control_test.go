package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Pause, steer, resume (K19): pause is a request the daemon honours at a
// boundary (202), steering needs a paused run, resume needs an instruction
// and continues the same session in a follow-up run; a finished run
// refuses; a run that ends before the daemon acks makes the pause moot.

func runCall(t *testing.T, h http.HandlerFunc, method, issueID, path string, body any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, h, testutil.WithURLParams(newRequest(method, "/api/issues/"+issueID+"/run/"+path, body), "id", issueID))
}

func TestRunPauseSteerResume(t *testing.T) {
	issue, task, agent := runningAgentRun(t, "pause")
	dbfx.Exec(t, `UPDATE agent_task_queue SET session_id = 'sess-1', work_dir = '/tmp/work' WHERE id = $1`, task)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_message WHERE task_id = $1`, task)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
	})
	type envelope struct {
		Run *RunControlState `json:"run"`
	}
	var out envelope
	// Steering before a pause is refused; a pause is accepted, not effective.
	if res := runCall(t, testHandler.SteerRun, http.MethodPost, issue, "steer", map[string]any{"instruction": "stop"}).Want(http.StatusBadRequest); res.Map()["code"] != ErrCodeRunNotPaused {
		t.Fatalf("steer before pause = %v", res.Map())
	}
	runCall(t, testHandler.PauseRun, http.MethodPost, issue, "pause", nil).Want(http.StatusAccepted).JSON(&out)
	if out.Run == nil || !out.Run.PausePending || out.Run.Status != "running" {
		t.Fatalf("pause request = %+v", out.Run)
	}
	// The daemon sees the flag on its status poll.
	var control struct {
		Status         string `json:"status"`
		PauseRequested bool   `json:"pause_requested"`
	}
	gateCall(t, testHandler.GetTaskStatus, http.MethodGet, "/api/daemon/tasks/"+task+"/status", nil, gateHeaders(task, agent), "taskId", task).Want(http.StatusOK).JSON(&control)
	if !control.PauseRequested || control.Status != "running" {
		t.Fatalf("daemon control = %+v", control)
	}
	// Daemon acks at the boundary with the session pointers.
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+task+"/paused", map[string]any{"session_id": "sess-2", "work_dir": "/tmp/work2", "branch_name": "feat/p"}, gateHeaders(task, agent), "taskId", task).Want(http.StatusOK)
	paused := mustTask(t, task)
	if paused.Status != "paused" || paused.PauseRequestedAt.Valid || paused.SessionID.String != "sess-2" || paused.BranchName.String != "feat/p" {
		t.Fatalf("paused task = %s pending=%v session=%s", paused.Status, paused.PauseRequestedAt.Valid, paused.SessionID.String)
	}
	// Resume without an instruction is refused; instructions are typed messages in seq order.
	if res := runCall(t, testHandler.ResumeRun, http.MethodPost, issue, "resume", nil).Want(http.StatusBadRequest); res.Map()["code"] != ErrCodeRunNoSteering {
		t.Fatalf("resume without instruction = %v", res.Map())
	}
	runCall(t, testHandler.SteerRun, http.MethodPost, issue, "steer", map[string]any{"instruction": "  "}).Want(http.StatusBadRequest)
	runCall(t, testHandler.SteerRun, http.MethodPost, issue, "steer", map[string]any{"instruction": "Use the existing helper instead of a new one"}).Want(http.StatusCreated)
	runCall(t, testHandler.SteerRun, http.MethodPost, issue, "steer", map[string]any{"instruction": "Add a test"}).Want(http.StatusCreated).JSON(&out)
	if len(out.Run.Instructions) != 2 || out.Run.Instructions[1] != "Add a test" {
		t.Fatalf("instructions = %v", out.Run.Instructions)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1 AND type = $2`, task, SteeringMessageType); n != 2 {
		t.Fatalf("steering messages = %d", n)
	}
	// Pausing an already paused run is a no-op 200; resume queues a follow-up on the same session.
	runCall(t, testHandler.PauseRun, http.MethodPost, issue, "pause", nil).Want(http.StatusOK)
	var resumed struct {
		Run          *RunControlState `json:"run"`
		PausedTaskID string           `json:"paused_task_id"`
	}
	runCall(t, testHandler.ResumeRun, http.MethodPost, issue, "resume", nil).Want(http.StatusCreated).JSON(&resumed)
	if resumed.PausedTaskID != task || resumed.Run == nil || resumed.Run.TaskID == task || resumed.Run.Status != "queued" {
		t.Fatalf("resume = %+v", resumed)
	}
	child := mustTask(t, resumed.Run.TaskID)
	if child.SessionID.String != "sess-2" || child.WorkDir.String != "/tmp/work2" || child.HandoffNote.String == "" {
		t.Fatalf("child session=%s workdir=%s note=%q", child.SessionID.String, child.WorkDir.String, child.HandoffNote.String)
	}
	if closed := mustTask(t, task); closed.Status != "paused" || uuidToString(closed.ResumedByTaskID) != resumed.Run.TaskID || !closed.CompletedAt.Valid {
		t.Fatalf("paused task after resume = %+v", closed.Status)
	}
	// A finished run refuses a pause with 409.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, resumed.Run.TaskID)
	if res := runCall(t, testHandler.PauseRun, http.MethodPost, issue, "pause", nil).Want(http.StatusConflict); res.Map()["code"] != ErrCodeRunNotRunning {
		t.Fatalf("pause on finished = %v", res.Map())
	}
}

func TestRunPauseIsMootWhenTheRunEndsFirst(t *testing.T) {
	issue, task, agent := runningAgentRun(t, "pause moot")
	runCall(t, testHandler.PauseRun, http.MethodPost, issue, "pause", nil).Want(http.StatusAccepted)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, task)
	var ack struct{ Paused bool }
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+task+"/paused", map[string]any{}, gateHeaders(task, agent), "taskId", task).Want(http.StatusOK).JSON(&ack)
	if ack.Paused || mustTask(t, task).Status != "completed" {
		t.Fatal("a run that ended first keeps its terminal status; the pause is ignored without error")
	}
}
