package service

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Budget settlement: the cap only works if every run's real cost reaches
// spent_usd_ticks exactly once.

type budgetFixture struct {
	routingTestFixture
	dbfx     *testutil.Fixture
	svc      *TaskService
	policyID string
}

// newBudgetFixture adds an enforced daily workspace policy far above anything
// the tests spend, and a task service wired to it.
func newBudgetFixture(t *testing.T) budgetFixture {
	t.Helper()
	fx, dbfx := fixedRoutingFixture(t)
	policyID := dbfx.Insert(t, "budget_policy", testutil.Cols{
		"workspace_id":    fx.workspace,
		"scope_type":      "workspace",
		"limit_usd_ticks": int64(1_000_000_000_000),
		"period":          "daily",
		"action":          "enforce",
		"created_by":      fx.user,
	})
	dbfx.Cleanup(t, `DELETE FROM budget_period WHERE policy_id = $1`, policyID)
	dbfx.Cleanup(t, `DELETE FROM budget_reservation WHERE policy_id = $1`, policyID)
	q := db.New(fx.pool)
	bus := events.New()
	svc := NewTaskService(q, fx.pool, nil, bus)
	svc.Budget = NewBudgetService(q, fx.pool, bus)
	svc.Budget.EstimateFloorTicks = 1_000
	return budgetFixture{routingTestFixture: fx, dbfx: dbfx, svc: svc, policyID: policyID}
}

func (b budgetFixture) reserve(t *testing.T, taskID string) {
	t.Helper()
	ctx := context.Background()
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := b.svc.Budget.ReserveTaskInTx(ctx, b.svc.Queries.WithTx(tx), BudgetScope{
		WorkspaceID: util.MustParseUUID(b.workspace), AgentID: util.MustParseUUID(b.agentID),
	}, util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// totals sums the policy's periods, so a cost charged into the wrong period
// or twice shows up too.
func (b budgetFixture) totals(t *testing.T) (spent, reserved int64) {
	t.Helper()
	b.dbfx.QueryRow(t, `SELECT COALESCE(SUM(spent_usd_ticks), 0)::bigint, COALESCE(SUM(reserved_usd_ticks), 0)::bigint
		FROM budget_period WHERE policy_id = $1`, b.policyID).Scan(&spent, &reserved)
	return spent, reserved
}

func (b budgetFixture) runningTask(t *testing.T, over testutil.Cols) string {
	t.Helper()
	issueID := b.dbfx.Issue(t, "budget run", testutil.Cols{"assignee_type": "agent", "assignee_id": b.agentID})
	cols := testutil.Cols{
		"runtime_id": b.runtimeA, "issue_id": issueID, "status": "running",
		"attempt": 1, "max_attempts": 1,
		"dispatched_at": testutil.Raw("now()"), "started_at": testutil.Raw("now()"),
	}
	for k, v := range over {
		cols[k] = v
	}
	return b.dbfx.Task(t, b.agentID, cols)
}

// A failed run burned real tokens; releasing its reservation without charging
// them let a failing agent spend forever under an enforced cap.
func TestBudgetFailedRunIsChargedItsReportedCost(t *testing.T) {
	b := newBudgetFixture(t)
	taskID := b.runningTask(t, nil)
	b.reserve(t, taskID)
	b.dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id": taskID, "provider": "anthropic", "model": "claude-x", "cost_usd_ticks": int64(5_000_000),
	})

	if _, err := b.svc.FailTask(context.Background(), util.MustParseUUID(taskID), "agent crashed", "", "", "", "agent_error", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	spent, reserved := b.totals(t)
	if spent != 5_000_000 || reserved != 0 {
		t.Fatalf("after a failed run spent=%d reserved=%d, want spent=5000000 reserved=0", spent, reserved)
	}
}

// A task still queued when the period rolls over is re-offered to
// ReserveTaskInTx at claim; it must keep its one reservation, or settlement
// charges its cost once per period it waited in.
func TestBudgetReservationIsNotDuplicatedAcrossAPeriodBoundary(t *testing.T) {
	b := newBudgetFixture(t)
	taskID := b.runningTask(t, testutil.Cols{"max_attempts": 1})
	day := time.Now().UTC().Truncate(24 * time.Hour)
	b.svc.Budget.Now = func() time.Time { return day.Add(-time.Minute) }
	b.reserve(t, taskID)
	b.svc.Budget.Now = func() time.Time { return day.Add(time.Minute) }
	b.reserve(t, taskID)

	if n := b.dbfx.Count(t, `SELECT COUNT(*) FROM budget_reservation WHERE task_id = $1`, taskID); n != 1 {
		t.Fatalf("task holds %d reservations, want 1", n)
	}
	b.dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id": taskID, "provider": "anthropic", "model": "claude-x", "cost_usd_ticks": int64(7_000_000),
	})
	if _, err := b.svc.CompleteTask(context.Background(), util.MustParseUUID(taskID), []byte(`{"output":"done"}`), "", "", "", false, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if spent, reserved := b.totals(t); spent != 7_000_000 || reserved != 0 {
		t.Fatalf("spent=%d reserved=%d across periods, want the run charged once: 7000000 / 0", spent, reserved)
	}
}

// A native run writes its usage after CompleteTask, so settlement at the
// terminal write saw none: every native run was charged zero.
func TestBudgetNativeRunIsChargedUsageRecordedAfterCompletion(t *testing.T) {
	b := newBudgetFixture(t)
	ctx := context.Background()
	taskID := b.runningTask(t, nil)
	b.reserve(t, taskID)
	if _, err := b.svc.CompleteTask(ctx, util.MustParseUUID(taskID), []byte(`{"output":"done"}`), "", "", "", false, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	native := NewNativeAgentService(b.svc.Queries, b.svc, nil, &scriptedNativeLLM{}, events.New())
	// runTask records usage exactly like this, after its terminal write.
	native.recordNativeUsage(ctx, util.MustParseUUID(taskID), nativeRunUsage{input: 100_000, output: 10_000, model: "gpt-4o"})

	spent, reserved := b.totals(t)
	if spent <= 0 || reserved != 0 {
		t.Fatalf("native run charged spent=%d reserved=%d, want its recorded usage priced and nothing held", spent, reserved)
	}
	native.recordNativeUsage(ctx, util.MustParseUUID(taskID), nativeRunUsage{input: 100_000, output: 10_000, model: "gpt-4o"})
	if again, _ := b.totals(t); again != spent {
		t.Fatalf("re-recording the same usage moved spent from %d to %d", spent, again)
	}
}
