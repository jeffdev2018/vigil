package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Replay lot 2 (K70): the run's snapshot at start, tool calls compared with
// the plan (drift), server-side redaction with a data class, and the safe
// replay whose writes are held whatever the agent's effect mode.

func TestRunReplaySnapshotDriftRedactionSafeMode(t *testing.T) {
	ctx := context.Background()
	agent := dbfx.Agent(t, "replay2 agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous", "effect_mode": "apply"})
	issue := dbfx.Issue(t, "replay2 issue "+uuid.NewString()[:8], testutil.Cols{"status": "todo", "assignee_type": "agent", "assignee_id": agent})
	dbfx.Insert(t, "issue_plan", testutil.Cols{"id": uuid.NewString(), "workspace_id": testWorkspaceID, "issue_id": issue, "version": 1,
		"content": "1. Read the file a.go with read_file. 2. Report.", "steps": `[{"id":"s1","title":"read file"}]`, "author_type": "agent", "author_id": agent})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_effect WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_plan WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1 AND id <> $2`, issue, task)
		testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE entity_id = $1`, task)
	})
	t0 := time.Now().Add(-5 * time.Minute)
	testHandler.recordRunSnapshot(ctx, mustTask(t, task), testWorkspaceID)
	dbfx.Insert(t, "task_message", testutil.Cols{"id": uuid.NewString(), "task_id": task, "seq": 1, "type": "text", "content": "key AKIAABCDEFGHIJKLMNOP leaked", "created_at": t0})
	dbfx.Insert(t, "task_message", testutil.Cols{"id": uuid.NewString(), "task_id": task, "seq": 2, "type": "tool_use", "tool": "read_file", "input": `{"path":"a.go"}`, "created_at": t0.Add(time.Minute)})
	dbfx.Insert(t, "task_message", testutil.Cols{"id": uuid.NewString(), "task_id": task, "seq": 3, "type": "tool_use", "tool": "write_file", "input": `{"path":"b.go"}`, "created_at": t0.Add(2 * time.Minute)})

	var out struct {
		Run struct {
			SafeMode bool `json:"safe_mode"`
			Snapshot *struct {
				TrustMode   string `json:"trust_mode"`
				EffectMode  string `json:"effect_mode"`
				PlanVersion int32  `json:"plan_version"`
			} `json:"snapshot"`
			Plan  *struct{ Version int32 } `json:"plan"`
			Drift int                      `json:"drift"`
		} `json:"run"`
		Events []struct {
			Kind      string `json:"kind"`
			Text      string `json:"text"`
			DataClass string `json:"data_class"`
			InPlan    *bool  `json:"in_plan"`
		} `json:"events"`
	}
	testutil.Call(t, testHandler.GetTaskReplay, testutil.WithURLParams(newRequest(http.MethodGet, "/api/tasks/"+task+"/replay", nil), "taskId", task)).Want(http.StatusOK).JSON(&out)
	if out.Run.Snapshot == nil || out.Run.Snapshot.TrustMode != "autonomous" || out.Run.Snapshot.EffectMode != "apply" || out.Run.Snapshot.PlanVersion != 1 {
		t.Fatalf("snapshot = %+v", out.Run.Snapshot)
	}
	if out.Run.Plan == nil || out.Run.Plan.Version != 1 || out.Run.Drift != 1 {
		t.Fatalf("plan v%v drift %d, want v1 and one call outside the plan", out.Run.Plan, out.Run.Drift)
	}
	var sawText, sawRead, sawWrite bool
	for _, e := range out.Events {
		switch {
		case e.Kind == "text":
			sawText = true
			if e.DataClass != "confidential" || e.Text != "key [REDACTED AWS KEY] leaked" {
				t.Fatalf("secret must be redacted server-side and classed confidential: %+v", e)
			}
		case e.Kind == "tool_use" && e.InPlan != nil && *e.InPlan:
			sawRead = true
		case e.Kind == "tool_use" && e.InPlan != nil && !*e.InPlan:
			sawWrite = true
		}
	}
	if !sawText || !sawRead || !sawWrite {
		t.Fatalf("expected a redacted text, an in-plan call and a drift call: text=%v read=%v write=%v", sawText, sawRead, sawWrite)
	}

	// Safe replay: a new run whose writes are held even though the agent applies.
	var sim struct {
		TaskID   string `json:"task_id"`
		SafeMode bool   `json:"safe_mode"`
	}
	testutil.Call(t, testHandler.SimulateTaskReplay, testutil.WithURLParams(newRequest(http.MethodPost, "/api/tasks/"+task+"/replay/simulate", nil), "taskId", task)).Want(http.StatusCreated).JSON(&sim)
	if !sim.SafeMode || dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE id = $1 AND safe_mode`, sim.TaskID) != 1 {
		t.Fatalf("simulate must enqueue a safe-mode run: %+v", sim)
	}
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, sim.TaskID, http.MethodPut, "/api/issues/"+issue, map[string]any{"status": "in_progress"}), "id", issue)).Want(http.StatusAccepted)
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&status)
	if status != "todo" || dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = 'pending'`, sim.TaskID) != 1 {
		t.Fatalf("a safe run holds its writes (status %s)", status)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.replayed_safe'`, task) != 1 {
		t.Fatal("the safe replay is audited on the source run")
	}
}
