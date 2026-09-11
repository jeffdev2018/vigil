package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A key failover (K48) replaces the retry reason gate and the attempt budget,
// never the shapes the retry machinery refuses: an autopilot run (its
// scheduler owns what happens after a failure, a retry child would run the
// work twice), a task whose max_attempts<=1 disables retries, or a run that
// is no longer active (a late /fail after a cancel must not retire keys).
func TestModelKeyFailoverRespectsRetryShapes(t *testing.T) {
	fx, dbfx := fixedRoutingFixture(t)
	ctx := context.Background()
	failovers := 0
	svc := NewTaskService(db.New(fx.pool), fx.pool, nil, events.New())
	svc.ModelKeyFailover = func(context.Context, db.AgentTaskQueue, string) bool {
		failovers++
		return true
	}
	fail := func(t *testing.T, over testutil.Cols) string {
		t.Helper()
		issueID := dbfx.Issue(t, "byok failover", testutil.Cols{"assignee_type": "agent", "assignee_id": fx.agentID})
		cols := testutil.Cols{
			"runtime_id": fx.runtimeA, "issue_id": issueID, "status": "running", "attempt": 1, "max_attempts": 2,
			"dispatched_at": testutil.Raw("now()"), "started_at": testutil.Raw("now()"),
		}
		for k, v := range over {
			cols[k] = v
		}
		taskID := dbfx.Task(t, fx.agentID, cols)
		dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE parent_task_id = $1`, taskID)
		_, _ = svc.FailTask(ctx, util.MustParseUUID(taskID), "401 invalid key", "", "", "", "agent_error.provider_auth_or_access", false, "", "")
		return taskID
	}
	children := func(t *testing.T, parentID string) int {
		t.Helper()
		return dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE parent_task_id = $1`, parentID)
	}

	if parent := fail(t, nil); children(t, parent) != 1 {
		t.Fatalf("control: an issue run failing over to the next key must retry once, children=%d", children(t, parent))
	}
	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": fx.workspace, "title": "byok autopilot", "assignee_id": fx.agentID,
		"execution_mode": "run_only", "created_by_type": "member", "created_by_id": fx.user,
	})
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{"autopilot_id": autopilotID, "source": "manual", "status": "running"})
	if parent := fail(t, testutil.Cols{"autopilot_run_id": runID, "issue_id": nil}); children(t, parent) != 0 {
		t.Fatal("an autopilot run was retried by the key failover behind its scheduler's back")
	}
	if parent := fail(t, testutil.Cols{"max_attempts": 1}); children(t, parent) != 0 {
		t.Fatal("a task with retries disabled (max_attempts=1) was revived by the key failover")
	}
	before := failovers
	fail(t, testutil.Cols{"status": "cancelled"})
	if failovers != before {
		t.Fatal("a late /fail on a cancelled run still retired the key")
	}
}
