package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
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
