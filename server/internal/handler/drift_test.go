package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// Drift detection (K40), DB path: a repeated call stops the run with its
// reason and an inbox item, a re-read loop too, a varied run is left alone,
// thresholds are per workspace and validated, message writes never fail
// because of it.

func toolCalls(t *testing.T, task, agent string, seq int, calls ...[2]string) {
	t.Helper()
	msgs := []map[string]any{}
	for i, c := range calls {
		msgs = append(msgs, map[string]any{"seq": seq + i, "type": "tool_use", "tool": c[0], "input": map[string]any{"file_path": c[1]}})
	}
	reportMessages(t, task, agent, msgs)
}

func TestDriftStopsRepeatedActionsAndRereadLoops(t *testing.T) {
	rememberSettings(t)
	var policy service.Drift
	poolCall(t, testHandler.PutDriftPolicy, http.MethodPut, "/api/drift-policy", map[string]any{"enabled": true, "repeated_action_threshold": 1, "file_reread_threshold": 3}).Want(http.StatusBadRequest)
	poolCall(t, testHandler.PutDriftPolicy, http.MethodPut, "/api/drift-policy", map[string]any{"enabled": true, "repeated_action_threshold": 3, "file_reread_threshold": 3}).Want(http.StatusOK).JSON(&policy)
	poolCall(t, testHandler.GetDriftPolicy, http.MethodGet, "/api/drift-policy", nil).Want(http.StatusOK).JSON(&policy)
	if policy.RepeatedActionThreshold != 3 || policy.FileRereadThreshold != 3 || !policy.Enabled {
		t.Fatalf("policy = %+v", policy)
	}
	_, repeat, agentR := runningAgentRun(t, "drift repeat")
	_, loop, agentL := runningAgentRun(t, "drift loop")
	_, fine, agentF := runningAgentRun(t, "drift fine")
	t.Cleanup(func() {
		for _, id := range []string{repeat, loop, fine} {
			testPool.Exec(context.Background(), `DELETE FROM task_message WHERE task_id = $1`, id)
		}
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE type = $1 AND workspace_id = $2`, DriftInboxType, testWorkspaceID)
	})
	// Same call three times: stopped, reason on the task, inbox item.
	toolCalls(t, repeat, agentR, 1, [2]string{"Bash", "x"}, [2]string{"Bash", "x"})
	if got := mustTask(t, repeat); got.Status != "running" {
		t.Fatal("two calls are under the threshold")
	}
	toolCalls(t, repeat, agentR, 3, [2]string{"Bash", "x"})
	got := mustTask(t, repeat)
	if got.Status != "failed" || got.FailureReason.String != service.ReasonDriftDetected || got.DriftReason.String != service.DriftRepeatedAction {
		t.Fatalf("repeated run = %s / %s / %s", got.Status, got.FailureReason.String, got.DriftReason.String)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND details->>'task_id' = $2`, DriftInboxType, repeat); n < 1 {
		t.Fatal("drift must reach the attention inbox")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1`, repeat); n != 3 {
		t.Fatalf("messages written = %d, detection must never block the write", n)
	}
	// Re-reading a.go three times without a write: stopped; a write in between resets.
	toolCalls(t, loop, agentL, 1, [2]string{"Read", "a.go"}, [2]string{"Edit", "a.go"}, [2]string{"Read", "a.go"}, [2]string{"Read", "b.go"}, [2]string{"Read", "a.go"})
	if mustTask(t, loop).Status != "running" {
		t.Fatal("two reads since the write are under the threshold")
	}
	toolCalls(t, loop, agentL, 6, [2]string{"Read", "a.go"})
	if got := mustTask(t, loop); got.DriftReason.String != service.DriftFileRereadLoop || got.Status != "failed" {
		t.Fatalf("loop run = %s / %s", got.Status, got.DriftReason.String)
	}
	// A varied run is never stopped.
	toolCalls(t, fine, agentF, 1, [2]string{"Read", "a.go"}, [2]string{"Edit", "a.go"}, [2]string{"Bash", "test"}, [2]string{"Read", "b.go"}, [2]string{"Write", "c.go"}, [2]string{"Read", "a.go"})
	if got := mustTask(t, fine); got.Status != "running" || got.DriftReason.Valid {
		t.Fatalf("normal run = %s / %v", got.Status, got.DriftReason.Valid)
	}
	resp := taskToResponse(mustTask(t, loop), testWorkspaceID)
	if resp.DriftReason != service.DriftFileRereadLoop {
		t.Fatal("the reason must be on the task response")
	}
}
