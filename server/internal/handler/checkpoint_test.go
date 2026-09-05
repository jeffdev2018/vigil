package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Checkpoints (K20): message reports advance the resume point, an
// infrastructure interruption resumes from it with a system message and an
// attempt count, the cap fails the run distinctly, a normal completion
// accumulates nothing, a local worktree run never leaves its daemon, and
// the cockpit endpoint tells the story.

func reportMessages(t *testing.T, task, agent string, msgs []map[string]any) {
	t.Helper()
	gateCall(t, testHandler.ReportTaskMessages, http.MethodPost, "/api/daemon/tasks/"+task+"/messages", map[string]any{"messages": msgs}, gateHeaders(task, agent), "taskId", task).Want(http.StatusOK)
}

func TestCheckpointsAdvanceResumeAndCap(t *testing.T) {
	issue, task, agent := runningAgentRun(t, "checkpoint")
	dbfx.Exec(t, `UPDATE agent_task_queue SET session_id = 'sess-cp' WHERE id = $1`, task)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE agent_id = $1)`, agent)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
	})
	ctx := context.Background()
	reportMessages(t, task, agent, []map[string]any{{"seq": 1, "type": "thinking", "content": "hm"}, {"seq": 2, "type": "tool_use", "tool": "read", "content": ""}})
	if got := mustTask(t, task); got.LastCheckpointSeq.Valid {
		t.Fatal("thinking and tool_use are not safe boundaries")
	}
	reportMessages(t, task, agent, []map[string]any{{"seq": 3, "type": "tool_result", "tool": "read", "content": "ok"}, {"seq": 4, "type": "text", "content": "done step"}})
	if got := mustTask(t, task); !got.LastCheckpointSeq.Valid || got.LastCheckpointSeq.Int64 != 4 || !got.CheckpointedAt.Valid {
		t.Fatalf("checkpoint = %+v", got.LastCheckpointSeq)
	}
	// Interruption: the retry child resumes from the checkpoint on the same session, attempt 1, with a system message on the parent.
	parentID := task
	for attempt := int32(1); attempt <= service.CheckpointResumeMaxAttempts; attempt++ {
		if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(parentID), "daemon went away", "sess-cp", "", "", "runtime_offline", false, "", ""); err != nil {
			t.Fatalf("fail attempt %d: %v", attempt, err)
		}
		var child db.AgentTaskQueue
		if err := testPool.QueryRow(ctx, `SELECT id, checkpoint_attempts, last_checkpoint_seq, session_id, status FROM agent_task_queue WHERE parent_task_id = $1`, parentID).Scan(&child.ID, &child.CheckpointAttempts, &child.LastCheckpointSeq, &child.SessionID, &child.Status); err != nil {
			t.Fatalf("attempt %d: no resume child: %v", attempt, err)
		}
		if child.CheckpointAttempts != attempt || child.LastCheckpointSeq.Int64 != 4 || child.SessionID.String != "sess-cp" {
			t.Fatalf("child %d = attempts %d seq %d session %s", attempt, child.CheckpointAttempts, child.LastCheckpointSeq.Int64, child.SessionID.String)
		}
		if n := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1 AND type = 'system' AND content LIKE 'Run interrupted%'`, parentID); n != 1 {
			t.Fatalf("attempt %d: system messages on the parent = %d", attempt, n)
		}
		parentID = uuidToString(child.ID)
		// The resumed run claims with the resume point in its payload.
		resp := taskToResponse(mustTask(t, parentID), testWorkspaceID)
		if resp.CheckpointAttempts != attempt {
			t.Fatalf("response attempts = %d", resp.CheckpointAttempts)
		}
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now(), max_attempts = 10 WHERE id = $1`, parentID)
	}
	// Beyond the cap: no child, a distinct failure reason.
	if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(parentID), "daemon went away again", "sess-cp", "", "", "runtime_offline", false, "", ""); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE parent_task_id = $1`, parentID); n != 0 {
		t.Fatal("the cap must stop the resume chain")
	}
	if got := mustTask(t, parentID); got.Status != "failed" || got.FailureReason.String != service.ReasonCheckpointResumeExhausted {
		t.Fatalf("exhausted run = %s / %s", got.Status, got.FailureReason.String)
	}
	var out struct {
		Run struct {
			Attempts  int32 `json:"attempts"`
			Exhausted bool  `json:"exhausted"`
			Status    string
		}
	}
	testutil.Call(t, testHandler.GetRunCheckpointStatus, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/run/checkpoint-status", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
	if !out.Run.Exhausted || out.Run.Attempts != service.CheckpointResumeMaxAttempts {
		t.Fatalf("checkpoint status = %+v", out.Run)
	}
}

func TestCheckpointLocalWorktreeStaysOnItsDaemonAndCompletionCountsNothing(t *testing.T) {
	local, other := poolRuntime(t, "cp local", "offline"), poolRuntime(t, "cp other", "online")
	dbfx.Exec(t, `UPDATE agent_runtime SET runtime_mode = 'local' WHERE id = $1`, local)
	pool := dbfx.Insert(t, "runtime_pool", testutil.Cols{"workspace_id": testWorkspaceID, "name": "cp", "runtime_ids": `["` + other + `"]`})
	agent := dbfx.Agent(t, "cp agent", local, testutil.Cols{"runtime_pool_id": pool})
	issue := dbfx.Issue(t, "cp issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": local, "issue_id": issue, "status": "running", "started_at": testutil.Raw("now()"), "work_dir": "/home/u/repo/.worktrees/x", "session_id": "s1"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE agent_id = $1)`, agent)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
		testPool.Exec(context.Background(), `DELETE FROM runtime_pool WHERE id = $1`, pool)
	})
	ctx := context.Background()
	if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(task), "offline", "s1", "/home/u/repo/.worktrees/x", "", "runtime_offline", false, "", ""); err != nil {
		t.Fatal(err)
	}
	var childRuntime string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent_task_queue WHERE parent_task_id = $1`, task).Scan(&childRuntime); err != nil {
		t.Fatalf("no resume child: %v", err)
	}
	if childRuntime != local {
		t.Fatalf("a local worktree run moved to %s; it must stay on its daemon %s", childRuntime, local)
	}
	// A run that completes normally never counts a resume attempt.
	done := dbfx.Task(t, agent, testutil.Cols{"runtime_id": other, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()")})
	if got := mustTask(t, done); got.CheckpointAttempts != 0 {
		t.Fatal("a normal completion accumulates no attempts")
	}
}
