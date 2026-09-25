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

// A run the sweepers fail (runtime offline, stale, orphaned) ends like one the
// daemon fails: its task token and run-scoped secrets stop working now, not
// at their expiry.
func TestHandleFailedTasksRevokesTaskTokensAndRunSecrets(t *testing.T) {
	fx, dbfx := fixedRoutingFixture(t)
	ctx := context.Background()
	issueID := dbfx.Issue(t, "swept run", testutil.Cols{"assignee_type": "agent", "assignee_id": fx.agentID})
	taskID := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id": fx.runtimeA, "issue_id": issueID, "status": "failed", "max_attempts": 1,
		"failure_reason": "runtime_offline", "completed_at": testutil.Raw("now()"),
	})
	dbfx.Insert(t, "task_token", testutil.Cols{
		"token_hash": "sweeper-token-" + taskID, "task_id": taskID, "agent_id": fx.agentID,
		"workspace_id": fx.workspace, "user_id": fx.user, "expires_at": time.Now().Add(24 * time.Hour),
	})
	dbfx.Insert(t, "run_scoped_secret", testutil.Cols{
		"workspace_id": fx.workspace, "task_id": taskID, "agent_id": fx.agentID, "key": "API_KEY",
		"token_hash": "sweeper-secret-" + taskID, "expires_at": time.Now().Add(30 * time.Minute),
	})
	task, err := db.New(fx.pool).GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}

	NewTaskService(db.New(fx.pool), fx.pool, nil, events.New()).HandleFailedTasks(ctx, []db.AgentTaskQueue{task})

	if n := dbfx.Count(t, `SELECT COUNT(*) FROM task_token WHERE task_id = $1`, taskID); n != 0 {
		t.Fatalf("swept run still holds %d task tokens", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM run_scoped_secret WHERE task_id = $1 AND revoked_at IS NULL`, taskID); n != 0 {
		t.Fatalf("swept run still holds %d live run secrets", n)
	}
}
