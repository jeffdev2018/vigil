package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Runtime pools (K28): CRUD with workspace checks, failover on infra
// failure through the pool in order, the degraded last resort made visible,
// a distinct failure when the pool is exhausted, moves at enqueue and for
// tasks waiting on a runtime that stays offline.

func poolRuntime(t *testing.T, name, status string) string {
	t.Helper()
	return dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": testWorkspaceID, "daemon_id": "pool-" + uuid.NewString()[:8], "name": name, "runtime_mode": "local",
		"provider": "claude", "status": status, "owner_id": testUserID, "last_seen_at": testutil.Raw("now() - interval '1 hour'"),
	})
}

func poolCall(t *testing.T, h http.HandlerFunc, method, path string, body any, params ...string) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(newRequest(method, path, body), "X-Workspace-ID", testWorkspaceID)
	return testutil.Call(t, h, testutil.WithURLParams(req, params...))
}

func TestRuntimePoolCRUDAndAssignment(t *testing.T) {
	a, b := poolRuntime(t, "pool a", "online"), poolRuntime(t, "pool b", "offline")
	foreignWS := dbfx.Workspace(t, "Pool foreign", "pool-foreign-"+uuid.NewString()[:8])
	foreign := dbfx.Insert(t, "agent_runtime", testutil.Cols{"workspace_id": foreignWS, "daemon_id": "x", "name": "foreign", "runtime_mode": "local", "provider": "claude", "status": "online", "owner_id": testUserID})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM runtime_pool WHERE workspace_id = $1`, testWorkspaceID)
	})

	poolCall(t, testHandler.CreateRuntimePool, http.MethodPost, "/api/runtime-pools", map[string]any{"name": "main", "runtime_ids": []string{a, foreign}}).Want(http.StatusUnprocessableEntity)
	poolCall(t, testHandler.CreateRuntimePool, http.MethodPost, "/api/runtime-pools", map[string]any{"name": "main", "runtime_ids": []string{}}).Want(http.StatusBadRequest)
	var pool RuntimePoolResponse
	poolCall(t, testHandler.CreateRuntimePool, http.MethodPost, "/api/runtime-pools", map[string]any{"name": "main", "runtime_ids": []string{a, b, a}, "degraded_runtime_id": b}).Want(http.StatusCreated).JSON(&pool)
	if strings.Join(pool.RuntimeIDs, ",") != a+","+b || pool.DegradedRuntimeID == nil || *pool.DegradedRuntimeID != b {
		t.Fatalf("pool = %+v", pool)
	}
	poolCall(t, testHandler.UpdateRuntimePool, http.MethodPut, "/api/runtime-pools/"+pool.ID, map[string]any{"runtime_ids": []string{b, a}}, "id", pool.ID).Want(http.StatusOK).JSON(&pool)
	if strings.Join(pool.RuntimeIDs, ",") != b+","+a || pool.Name != "main" || pool.DegradedRuntimeID != nil {
		t.Fatalf("updated pool = %+v, want reordered, name kept, degraded cleared", pool)
	}
	viewer := dbfx.User(t, "Pool viewer", "pool-viewer@multica.ai")
	dbfx.Member(t, testWorkspaceID, viewer, "member")
	req := testutil.WithHeaders(newRequest(http.MethodDelete, "/api/runtime-pools/"+pool.ID, nil), "X-Workspace-ID", testWorkspaceID, "X-User-ID", viewer)
	testutil.Call(t, testHandler.DeleteRuntimePool, testutil.WithURLParams(req, "id", pool.ID)).Want(http.StatusForbidden)

	agent := dbfx.Agent(t, "pool agent", a)
	var resp AgentResponse
	poolCall(t, testHandler.SetAgentRuntimePool, http.MethodPut, "/api/agents/"+agent+"/runtime-pool", map[string]any{"pool_id": pool.ID}, "id", agent).Want(http.StatusOK).JSON(&resp)
	if resp.RuntimePoolID == nil || *resp.RuntimePoolID != pool.ID {
		t.Fatalf("agent pool = %v", resp.RuntimePoolID)
	}
	poolCall(t, testHandler.DeleteRuntimePool, http.MethodDelete, "/api/runtime-pools/"+pool.ID, nil, "id", pool.ID).Want(http.StatusConflict)
	poolCall(t, testHandler.SetAgentRuntimePool, http.MethodPut, "/api/agents/"+agent+"/runtime-pool", map[string]any{"pool_id": nil}, "id", agent).Want(http.StatusOK)
	poolCall(t, testHandler.DeleteRuntimePool, http.MethodDelete, "/api/runtime-pools/"+pool.ID, nil, "id", pool.ID).Want(http.StatusNoContent)
}

func TestRuntimePoolFailsOverInOrderThenDegradedThenExhausts(t *testing.T) {
	a, b, c, d := poolRuntime(t, "fo a", "offline"), poolRuntime(t, "fo b", "online"), poolRuntime(t, "fo c", "offline"), poolRuntime(t, "fo local", "online")
	pool := dbfx.Insert(t, "runtime_pool", testutil.Cols{"workspace_id": testWorkspaceID, "name": "fo", "runtime_ids": `["` + b + `","` + c + `"]`, "degraded_runtime_id": d})
	agent := dbfx.Agent(t, "fo agent", a, testutil.Cols{"runtime_pool_id": pool})
	issue := dbfx.Issue(t, "fo issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": a, "issue_id": issue, "status": "running", "started_at": testutil.Raw("now()")})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
		testPool.Exec(context.Background(), `DELETE FROM runtime_pool WHERE id = $1`, pool)
	})
	ctx := context.Background()
	fail := func(taskID string, reason string) db.AgentTaskQueue {
		t.Helper()
		if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(taskID), "provider down", "", "", "", reason, false, "", ""); err != nil {
			t.Fatalf("fail %s: %v", taskID, err)
		}
		var child db.AgentTaskQueue
		err := testPool.QueryRow(ctx, `SELECT id, runtime_id, status, failover_history, attempt, max_attempts FROM agent_task_queue WHERE parent_task_id = $1`, taskID).Scan(&child.ID, &child.RuntimeID, &child.Status, &child.FailoverHistory, &child.Attempt, &child.MaxAttempts)
		if err != nil {
			return db.AgentTaskQueue{}
		}
		return child
	}
	// An application failure never moves runtime.
	if child := fail(task, "agent_error"); child.ID.Valid {
		t.Fatalf("agent_error must not fail over: %+v", child)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', failure_reason = NULL, completed_at = NULL WHERE id = $1`, task)
	// Provider outage: a → b (first online in order, c is offline).
	child := fail(task, "agent_error.provider_server_error")
	moves := func(raw []byte) []service.FailoverEntry {
		var out []service.FailoverEntry
		_ = json.Unmarshal(raw, &out)
		return out
	}
	if m := moves(child.FailoverHistory); !child.ID.Valid || uuidToString(child.RuntimeID) != b || child.Status != "queued" || len(m) != 1 || m[0].To != b || m[0].From != a {
		t.Fatalf("first failover = %+v", child)
	}
	if service.TaskDegraded(child.FailoverHistory) {
		t.Fatal("b is not the degraded runtime")
	}
	// b fails too: c is offline, so the degraded local model takes over, visibly.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, uuidToString(child.ID))
	grand := fail(uuidToString(child.ID), "runtime_offline")
	if !grand.ID.Valid || uuidToString(grand.RuntimeID) != d || !service.TaskDegraded(grand.FailoverHistory) || grand.Status != "queued" {
		t.Fatalf("degraded failover = %+v (history %s)", grand, grand.FailoverHistory)
	}
	if grand.Attempt >= grand.MaxAttempts {
		t.Fatalf("failover child must keep room in its budget: attempt %d / max %d", grand.Attempt, grand.MaxAttempts)
	}
	// Nothing left: the run fails with the pool's own reason, no child.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, uuidToString(grand.ID))
	if last := fail(uuidToString(grand.ID), "agent_error.provider_network"); last.ID.Valid {
		t.Fatalf("exhausted pool must not retry: %+v", last)
	}
	var reason string
	testPool.QueryRow(ctx, `SELECT failure_reason FROM agent_task_queue WHERE id = $1`, uuidToString(grand.ID)).Scan(&reason)
	if reason != service.ReasonRuntimePoolExhausted {
		t.Fatalf("failure_reason = %q, want %s", reason, service.ReasonRuntimePoolExhausted)
	}
	// The history endpoint tells the story, degraded flagged.
	var out struct {
		Runs []TaskFailoverResponse `json:"runs"`
	}
	testutil.Call(t, testHandler.ListIssueFailoverHistory, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/failover-history", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
	if len(out.Runs) != 2 || !out.Runs[0].Degraded || len(out.Runs[0].Moves) != 2 || out.Runs[0].Reason != service.ReasonRuntimePoolExhausted {
		t.Fatalf("history = %+v", out.Runs)
	}
}

func TestRuntimePoolMovesWaitingTasksAndEnqueues(t *testing.T) {
	a, b := poolRuntime(t, "wait a", "offline"), poolRuntime(t, "wait b", "online")
	pool := dbfx.Insert(t, "runtime_pool", testutil.Cols{"workspace_id": testWorkspaceID, "name": "wait", "runtime_ids": `["` + b + `"]`})
	agent := dbfx.Agent(t, "wait agent", a, testutil.Cols{"runtime_pool_id": pool})
	issue := dbfx.Issue(t, "wait issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	queued := dbfx.Task(t, agent, testutil.Cols{"runtime_id": a, "issue_id": issue, "status": "queued"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
		testPool.Exec(context.Background(), `DELETE FROM runtime_pool WHERE id = $1`, pool)
	})
	ctx := context.Background()
	if moved := testHandler.TaskService.MoveWaitingTasksOffOfflineRuntimes(ctx, 0, 10); moved < 1 {
		t.Fatalf("moved = %d, want the queued task", moved)
	}
	var rt string
	testPool.QueryRow(ctx, `SELECT runtime_id FROM agent_task_queue WHERE id = $1`, queued).Scan(&rt)
	if rt != b {
		t.Fatalf("waiting task runtime = %s, want %s", rt, b)
	}
	// A second sweep has nothing left to move for this task.
	dbfx.Exec(t, `DELETE FROM agent_task_queue WHERE id = $1`, queued)
	// Enqueue on an offline runtime goes straight to the pool.
	issueRow, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue))
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issueRow)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var moves []service.FailoverEntry
	_ = json.Unmarshal(task.FailoverHistory, &moves)
	if uuidToString(task.RuntimeID) != b || len(moves) != 1 || moves[0].Reason != "runtime_offline" {
		t.Fatalf("enqueued task = runtime %s history %s", uuidToString(task.RuntimeID), task.FailoverHistory)
	}
}
