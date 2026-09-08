package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Workflow execution safety (JEF-275): cancellation settles a run like any
// other terminal transition, a workflow stops growing at its ceiling, and a
// trigger for an unroutable agent is refused with the reason on the record.

// TestCancelledRunCleanup: a running preview-mode task holding a write is
// cancelled. The held write must not stay pending forever, the run's replay
// chain must be sealed, and the settlement must be on the record.
func TestCancelledRunCleanup(t *testing.T) {
	ctx := context.Background()
	agent := dbfx.Agent(t, "cancel cleanup agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous", "effect_mode": "preview"})
	issue := dbfx.Issue(t, "cancel cleanup issue "+uuid.NewString()[:8], testutil.Cols{"status": "todo", "priority": "medium"})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running", "started_at": "now()"})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_effect WHERE task_id = $1`, task)
		testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE entity_id = $1`, task)
	})

	// The run holds one write: 202, the issue unchanged, one pending effect.
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPut, "/api/issues/"+issue, map[string]any{"status": "in_progress"}), "id", issue)).Want(http.StatusAccepted)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = 'pending'`, task); n != 1 {
		t.Fatalf("held writes pending before the cancel = %d, want 1", n)
	}

	if _, err := testHandler.TaskService.CancelTaskByUser(ctx, parseUUID(task)); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = 'pending'`, task); n != 0 {
		t.Fatalf("a cancelled run must not leave %d held write(s) pending", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = $2`, task, service.EffectRejected); n != 1 {
		t.Fatalf("the held write must be dropped, rejected rows = %d", n)
	}
	// The issue must be untouched: dropping a held write never applies it.
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&status)
	if status != "todo" {
		t.Fatalf("issue status after a cancelled preview run = %q, want todo", status)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = $2 AND (details->>'dropped_effects')::int = 1`, task, AuditRunCancelledCleanup); n != 1 {
		t.Fatalf("the cancellation settlement must be audited with its dropped count, rows = %d", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = $2`, task, AuditRunSealed); n != 1 {
		t.Fatalf("a cancelled run's replay chain must be sealed, run.sealed rows = %d", n)
	}
}

// TestCancelledQueuedRunIsNotSealed: a run cancelled before it ever started has
// no event chain, so it must not add an empty replay to the audit log.
func TestCancelledQueuedRunIsNotSealed(t *testing.T) {
	ctx := context.Background()
	agent := dbfx.Agent(t, "cancel queued agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "cancel queued issue "+uuid.NewString()[:8])
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "queued"})
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE entity_id = $1`, task) })

	if _, err := testHandler.TaskService.CancelTaskByUser(ctx, parseUUID(task)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action IN ($2, $3)`, task, AuditRunSealed, AuditRunCancelledCleanup); n != 0 {
		t.Fatalf("a never-started run has nothing to settle, audit rows = %d", n)
	}
}

// TestWorkflowLimitsRefuseLeg: a workflow at its leg ceiling refuses to grow,
// and the refusal is auditable with the counts that justify it.
func TestWorkflowLimitsRefuseLeg(t *testing.T) {
	ctx := context.Background()
	rememberSettings(t)
	agent := dbfx.Agent(t, "workflow limits agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "workflow limits issue "+uuid.NewString()[:8])
	root := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed"})
	leg := dbfx.Task(t, agent, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed",
		"leg_role": service.LegRoleReview, "workflow_root_task_id": root,
	})
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE entity_id = $1`, root) })

	setWorkflowLimits(t, 2, 0)
	// Two legs exist and the ceiling is two: a third is refused, whichever leg
	// of the workflow the producer happens to hold.
	for _, parent := range []string{root, leg} {
		ok, reason := testHandler.TaskService.WorkflowAllowsLeg(ctx, mustTask(t, parent), service.LegRoleRevision)
		if ok {
			t.Fatalf("a third leg must be refused at max_legs=2 (parent %s)", parent)
		}
		if reason != "max_legs" {
			t.Fatalf("refusal reason = %q, want max_legs", reason)
		}
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = $2 AND (details->>'legs')::int = 2 AND details->>'role' = $3`, root, service.AuditWorkflowLegRefused, service.LegRoleRevision); n != 2 {
		t.Fatalf("each refusal must be audited against the workflow root, rows = %d", n)
	}

	// Raising the ceiling lets the same workflow grow again: the bound is the
	// setting, not a permanent mark on the workflow.
	setWorkflowLimits(t, 8, 0)
	if ok, reason := testHandler.TaskService.WorkflowAllowsLeg(ctx, mustTask(t, root), service.LegRoleRevision); !ok {
		t.Fatalf("a third leg must be allowed at max_legs=8, refused with %q", reason)
	}

	// A producer with no parent run has no workflow to bound.
	if ok, _ := testHandler.TaskService.WorkflowAllowsLeg(ctx, db.AgentTaskQueue{}, service.LegRoleFanout); !ok {
		t.Fatal("a leg that is its own root is always allowed")
	}
}

// TestWorkflowLimitsCostCeiling: the cost ceiling refuses independently of the
// leg count, and zero means unlimited.
func TestWorkflowLimitsCostCeiling(t *testing.T) {
	ctx := context.Background()
	rememberSettings(t)
	agent := dbfx.Agent(t, "workflow cost agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "workflow cost issue "+uuid.NewString()[:8])
	root := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed"})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id": root, "provider": "claude", "model": "sonnet",
		"input_tokens": 10, "output_tokens": 10, "cost_usd_ticks": 5000,
	})
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE entity_id = $1`, root) })

	setWorkflowLimits(t, 50, 5000)
	ok, reason := testHandler.TaskService.WorkflowAllowsLeg(ctx, mustTask(t, root), service.LegRoleRetry)
	if ok || reason != "max_cost_usd_ticks" {
		t.Fatalf("a workflow at its cost ceiling must refuse; ok=%v reason=%q", ok, reason)
	}
	setWorkflowLimits(t, 50, 0)
	if ok, reason := testHandler.TaskService.WorkflowAllowsLeg(ctx, mustTask(t, root), service.LegRoleRetry); !ok {
		t.Fatalf("max_cost_usd_ticks=0 means unlimited, refused with %q", reason)
	}
}

// TestWorkflowLimitsEndpoint: the setting round-trips and rejects out-of-range
// values rather than silently clamping them.
func TestWorkflowLimitsEndpoint(t *testing.T) {
	rememberSettings(t)
	var got map[string]any
	testutil.Call(t, testHandler.GetWorkflowLimits, newRequest(http.MethodGet, "/api/workflow-limits", nil)).Want(http.StatusOK).JSON(&got)
	if got["max_legs"] != float64(8) {
		t.Fatalf("default max_legs = %v, want 8", got["max_legs"])
	}
	testutil.Call(t, testHandler.PutWorkflowLimits, newRequest(http.MethodPut, "/api/workflow-limits", map[string]any{"max_legs": 0})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutWorkflowLimits, newRequest(http.MethodPut, "/api/workflow-limits", map[string]any{"max_legs": 51})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutWorkflowLimits, newRequest(http.MethodPut, "/api/workflow-limits", map[string]any{"max_legs": 3, "max_cost_usd_ticks": -1})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutWorkflowLimits, newRequest(http.MethodPut, "/api/workflow-limits", map[string]any{"max_legs": 3, "max_cost_usd_ticks": 900})).Want(http.StatusOK)
	testutil.Call(t, testHandler.GetWorkflowLimits, newRequest(http.MethodGet, "/api/workflow-limits", nil)).Want(http.StatusOK).JSON(&got)
	if got["max_legs"] != float64(3) || got["max_cost_usd_ticks"] != float64(900) {
		t.Fatalf("workflow limits did not round-trip: %v", got)
	}
}

func setWorkflowLimits(t *testing.T, maxLegs int, maxCost int64) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || jsonb_build_object('workflow_limits', jsonb_build_object('max_legs', $2::int, 'max_cost_usd_ticks', $3::bigint)) WHERE id = $1`, testWorkspaceID, maxLegs, maxCost)
}
