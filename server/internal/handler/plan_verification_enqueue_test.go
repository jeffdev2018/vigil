package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F17: exactly one verification run per completed run on an issue with an
// active plan, only with the gate on, and never for a verification run.

// completedAgentRun builds an agent-assigned issue with a completed run on it
// and returns (issueID, taskID).
func completedAgentRun(t *testing.T, label string) (string, string) {
	t.Helper()
	agentID := dbfx.Agent(t, label+" agent", handlerTestRuntimeID(t), testutil.Cols{
		"instructions": "",
		"custom_env":   testutil.Raw("'{}'::jsonb"),
		"custom_args":  testutil.Raw("'[]'::jsonb"),
	})
	issueID := dbfx.Issue(t, label+" issue", testutil.Cols{
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   handlerTestRuntimeID(t),
		"issue_id":     issueID,
		"status":       "completed",
		"started_at":   testutil.Raw("now()"),
		"completed_at": testutil.Raw("now()"),
	})
	return issueID, taskID
}

func countTasksOnIssue(t *testing.T, issueID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issueID)
}

func TestMaybeEnqueuePlanVerificationQueuesOneRun(t *testing.T) {
	setPlanVerificationGate(t, true)
	issue, task := completedAgentRun(t, "verify enqueue")
	plan := putPlan(t, issue, "1. add endpoint\n2. test it")
	ctx := context.Background()

	if err := testHandler.TaskService.MaybeEnqueuePlanVerification(ctx, parseUUID(task)); err != nil {
		t.Fatal(err)
	}
	if n := countTasksOnIssue(t, issue); n != 2 {
		t.Fatalf("tasks on issue = %d, want the completed run plus one verification", n)
	}
	var verificationTask, note string
	var version int32
	dbfx.QueryRow(t, `SELECT v.task_id, v.plan_version, q.handoff_note FROM plan_verification v JOIN agent_task_queue q ON q.id = v.task_id WHERE v.source_task_id = $1`, task).
		Scan(&verificationTask, &version, &note)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, verificationTask) })
	if version != plan.Plan.Version || !strings.HasPrefix(note, service.PlanVerificationHandoffPrefix) || !strings.Contains(note, "add endpoint") {
		t.Fatalf("verification (v%d, note %q), want the active plan carried in the handoff note", version, note)
	}

	// Replay of the same completion queues nothing more.
	if err := testHandler.TaskService.MaybeEnqueuePlanVerification(ctx, parseUUID(task)); err != nil {
		t.Fatal(err)
	}
	if n := countTasksOnIssue(t, issue); n != 2 {
		t.Fatalf("tasks after replay = %d, want still 2", n)
	}

	// The verification run completing never spawns another verification.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, verificationTask)
	if err := testHandler.TaskService.MaybeEnqueuePlanVerification(ctx, parseUUID(verificationTask)); err != nil {
		t.Fatal(err)
	}
	if n := countTasksOnIssue(t, issue); n != 2 {
		t.Fatalf("tasks after verification completed = %d, want still 2 (no loop)", n)
	}
}

func TestMaybeEnqueuePlanVerificationSkipsWithoutGateOrPlan(t *testing.T) {
	ctx := context.Background()

	setPlanVerificationGate(t, false)
	issue, task := completedAgentRun(t, "verify gate off")
	putPlan(t, issue, "a plan")
	if err := testHandler.TaskService.MaybeEnqueuePlanVerification(ctx, parseUUID(task)); err != nil {
		t.Fatal(err)
	}
	if n := countTasksOnIssue(t, issue); n != 1 {
		t.Fatalf("gate off queued a verification: %d tasks", n)
	}

	setPlanVerificationGate(t, true)
	noPlan, task2 := completedAgentRun(t, "verify no plan")
	if err := testHandler.TaskService.MaybeEnqueuePlanVerification(ctx, parseUUID(task2)); err != nil {
		t.Fatal(err)
	}
	if n := countTasksOnIssue(t, noPlan); n != 1 {
		t.Fatalf("issue without plan queued a verification: %d tasks", n)
	}
}

// Regression test for the non-atomic create+enqueue bug: MaybeEnqueuePlanVerification
// now writes the plan_verification tracking row (task_id = source_task_id,
// a placeholder) BEFORE calling EnqueueTaskForIssueWithHandoff, specifically
// so a transient failure between the two can never leave a completed source
// task without a row that marks it as handled -- which is what let a
// duplicate, uncontrolled verification run get queued on a later retry.
// This asserts the mechanism the fix relies on directly: as soon as that
// placeholder row exists, PlanVerificationExistsForSource already reports
// true for the source task, before any verification run has been enqueued.
func TestPlanVerificationPlaceholderRowClosesReFireWindowBeforeEnqueue(t *testing.T) {
	setPlanVerificationGate(t, true)
	issue, task := completedAgentRun(t, "verify placeholder")
	plan := putPlan(t, issue, "1. step")
	ctx := context.Background()

	if exists, err := testHandler.Queries.PlanVerificationExistsForSource(ctx, parseUUID(task)); err != nil || exists {
		t.Fatalf("exists = %v %v, want false before any verification row", exists, err)
	}

	row, err := testHandler.Queries.CreatePlanVerification(ctx, db.CreatePlanVerificationParams{
		WorkspaceID:  parseUUID(testWorkspaceID),
		IssueID:      parseUUID(issue),
		PlanID:       parseUUID(plan.Plan.ID),
		PlanVersion:  plan.Plan.Version,
		TaskID:       parseUUID(task), // placeholder: same as source_task_id
		SourceTaskID: parseUUID(task),
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM plan_verification WHERE id = $1`, uuidToString(row.ID))
	})
	if err != nil {
		t.Fatalf("CreatePlanVerification (placeholder): %v", err)
	}

	// The guard MaybeEnqueuePlanVerification checks on every completion must
	// already see this source task as handled -- no verification run has
	// been enqueued yet.
	exists, err := testHandler.Queries.PlanVerificationExistsForSource(ctx, parseUUID(task))
	if err != nil || !exists {
		t.Fatalf("exists = %v %v, want true right after the placeholder row is written", exists, err)
	}
	if n := countTasksOnIssue(t, issue); n != 1 {
		t.Fatalf("tasks on issue = %d, want still just the source run (nothing enqueued yet)", n)
	}
}
