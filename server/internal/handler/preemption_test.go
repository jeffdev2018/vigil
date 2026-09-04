package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Preemption (K41): an urgent issue queued on a saturated agent suspends
// its lowest-priority running run (a pause at the next boundary), never an
// urgent one; suspended runs resume on their own from their checkpoint
// once capacity frees, priority first then age; the history is readable
// on the suspended issue; a human may resume a preempted run without an
// instruction.

func TestPreemptionSuspendsLowestPriorityAndResumesInOrder(t *testing.T) {
	agent := dbfx.Agent(t, "preempt agent", handlerTestRuntimeID(t), testutil.Cols{"max_concurrent_tasks": 1})
	low := dbfx.Issue(t, "background chore", testutil.Cols{"status": "in_progress", "priority": "low", "assignee_type": "agent", "assignee_id": agent})
	lowTask := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": low, "status": "running", "priority": 1, "started_at": testutil.Raw("now()"), "session_id": "sess-low", "work_dir": "/w/low"})
	urgent := dbfx.Issue(t, "prod is down", testutil.Cols{"status": "todo", "priority": "urgent", "assignee_type": "agent", "assignee_id": agent})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE agent_id = $1)`, agent)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
	})
	ctx := context.Background()
	issueRow, _ := testHandler.Queries.GetIssue(ctx, parseUUID(urgent))
	urgentTask, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issueRow)
	if err != nil {
		t.Fatalf("enqueue urgent: %v", err)
	}
	victim := mustTask(t, lowTask)
	if !victim.PreemptedAt.Valid || uuidToString(victim.PreemptedByTaskID) != uuidToString(urgentTask.ID) || !victim.PauseRequestedAt.Valid || victim.Status != "running" {
		t.Fatalf("victim = preempted %v by %s pause %v status %s", victim.PreemptedAt.Valid, uuidToString(victim.PreemptedByTaskID), victim.PauseRequestedAt.Valid, victim.Status)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1 AND type = 'system' AND content LIKE 'Suspended%'`, lowTask); n != 1 {
		t.Fatal("the suspension must be written on the run")
	}
	// The daemon pauses at the boundary. Meanwhile an urgent run is never a victim.
	gateCall(t, testHandler.AckTaskPaused, http.MethodPost, "/api/daemon/tasks/"+lowTask+"/paused", map[string]any{"session_id": "sess-low"}, gateHeaders(lowTask, agent), "taskId", lowTask).Want(http.StatusOK)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, uuidToString(urgentTask.ID))
	urgent2 := dbfx.Issue(t, "also urgent", testutil.Cols{"status": "todo", "priority": "urgent", "assignee_type": "agent", "assignee_id": agent})
	issue2, _ := testHandler.Queries.GetIssue(ctx, parseUUID(urgent2))
	if _, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue2); err != nil {
		t.Fatal(err)
	}
	if got := mustTask(t, uuidToString(urgentTask.ID)); got.PreemptedAt.Valid || got.PauseRequestedAt.Valid {
		t.Fatal("an urgent run is never preempted")
	}
	// No capacity yet (the urgent run is running): nothing resumes.
	if n := testHandler.TaskService.ResumePreemptedTasks(ctx, 10); n != 0 {
		t.Fatalf("resumed %d while saturated", n)
	}
	// History on the suspended issue names the urgent issue.
	var hist struct{ Preemptions []PreemptionResponse }
	testutil.Call(t, testHandler.ListIssuePreemptions, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+low+"/preemptions", nil), "id", low)).Want(http.StatusOK).JSON(&hist)
	if len(hist.Preemptions) != 1 || hist.Preemptions[0].PreemptedByIssueID == nil || *hist.Preemptions[0].PreemptedByIssueID != urgent || hist.Preemptions[0].PreemptedByIdentifier == nil || hist.Preemptions[0].ResumedByTaskID != nil {
		t.Fatalf("history = %+v", hist.Preemptions)
	}
	// Capacity frees (urgent done, the second urgent still queued keeps the slot): still nothing.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, uuidToString(urgentTask.ID))
	if n := testHandler.TaskService.ResumePreemptedTasks(ctx, 10); n != 0 {
		t.Fatal("a queued urgent run takes the freed slot first")
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'cancelled', completed_at = now() WHERE issue_id = $1`, urgent2)
	// Two suspended runs, capacity for one: higher priority first.
	medium := dbfx.Issue(t, "medium chore", testutil.Cols{"status": "in_progress", "priority": "medium", "assignee_type": "agent", "assignee_id": agent})
	mediumTask := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": medium, "status": "paused", "priority": 2, "started_at": testutil.Raw("now()"), "preempted_at": testutil.Raw("now()"), "preempted_by_task_id": uuidToString(urgentTask.ID), "session_id": "sess-med"})
	if n := testHandler.TaskService.ResumePreemptedTasks(ctx, 10); n != 1 {
		t.Fatalf("resumed %d, want exactly one (capacity 1)", n)
	}
	if got := mustTask(t, mediumTask); !got.ResumedByTaskID.Valid {
		t.Fatal("the medium run must resume before the low one")
	}
	if got := mustTask(t, lowTask); got.ResumedByTaskID.Valid {
		t.Fatal("the low run waits its turn")
	}
	child := mustTask(t, uuidToString(mustTask(t, mediumTask).ResumedByTaskID))
	if child.SessionID.String != "sess-med" || child.Status != "queued" || child.HandoffNote.String == "" {
		t.Fatalf("resumed child = session %s status %s note %q", child.SessionID.String, child.Status, child.HandoffNote.String)
	}
	// A human may resume the remaining preempted run without leaving an instruction.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, uuidToString(child.ID))
	runCall(t, testHandler.ResumeRun, http.MethodPost, low, "resume", nil).Want(http.StatusCreated)
	if got := mustTask(t, lowTask); !got.ResumedByTaskID.Valid {
		t.Fatal("manual resume of a preempted run must work without an instruction")
	}
}
