package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type unavailableMemoryDB struct{ db.DBTX }

func (q unavailableMemoryDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "-- name: ListRecentAgentMemories") {
		return nil, errors.New("memory read unavailable")
	}
	return q.DBTX.Query(ctx, sql, args...)
}

func TestTaskMemoryContextClaimVersionsAndReclaim(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%v", batch), func(t *testing.T) {
			ctx := context.Background()
			daemon := "memory-context-daemon"
			runtime := dbfx.Runtime(t, "Memory context runtime", testutil.Cols{"daemon_id": daemon})
			agent := dbfx.Agent(t, "Memory context agent", runtime)
			project := dbfx.Project(t, "Memory context project", testutil.Cols{"memory_rules": `["Use pnpm"]`, "memory_revision": 3})
			issue := dbfx.Issue(t, "Memory context issue", testutil.Cols{"project_id": project})
			taskID := dbfx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": issue})
			t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM task_token WHERE task_id=$1`, taskID) })
			var ids []string
			for i := 0; i < 51; i++ {
				ids = append(ids, dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "content": fmt.Sprintf("Fact %d", i), "source": "manual", "status": "active", "revision": i + 1, "created_at": time.Now().Add(time.Duration(i-100) * time.Minute)}))
			}
			dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "content": "Pending fact", "source": "run", "status": "pending"})
			dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "content": "Expired fact", "source": "manual", "status": "active", "expires_at": time.Now().Add(-time.Hour)})
			foreign := dbfx.Workspace(t, "Foreign memory context", "foreign-memory-context")
			dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": foreign, "agent_id": agent, "content": "Foreign fact", "source": "manual", "status": "active"})
			claim := func() AgentTaskResponse {
				t.Helper()
				if batch {
					req := newDaemonTokenRequest("POST", "/api/daemon/claim", map[string]any{"daemon_id": daemon, "runtime_ids": []string{runtime}, "max_tasks": 1}, testWorkspaceID, daemon)
					var result struct {
						Tasks []AgentTaskResponse `json:"tasks"`
					}
					testutil.Call(t, testHandler.ClaimTasksByRuntime, req).Want(http.StatusOK).JSON(&result)
					if len(result.Tasks) != 1 {
						t.Fatalf("expected one task: %+v", result)
					}
					return result.Tasks[0]
				}
				req := withURLParam(newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtime+"/claim", nil, testWorkspaceID, daemon), "runtimeId", runtime)
				var result struct {
					Task *AgentTaskResponse `json:"task"`
				}
				testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&result)
				if result.Task == nil {
					t.Fatal("no claimed task")
				}
				return *result.Task
			}
			first := claim()
			receipt := first.MemoryContext
			if receipt == nil || receipt.AgentStatus != "loaded" || len(receipt.AgentVersions) != 50 || receipt.ProjectVersion == nil || receipt.ProjectVersion.ID != project || receipt.ProjectVersion.Revision != 3 {
				t.Fatalf("wrong context: %+v", receipt)
			}
			for i, version := range receipt.AgentVersions {
				if version.ID != ids[i+1] || version.Revision != int32(i+2) || first.Agent.Memories[i] != fmt.Sprintf("Fact %d", i+1) {
					t.Fatalf("version/content mismatch at %d: %+v", i, version)
				}
			}
			stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			assertStored := func(want *service.TaskMemoryContext) {
				t.Helper()
				row, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
				if err != nil {
					t.Fatal(err)
				}
				var got service.TaskMemoryContext
				if err = json.Unmarshal(row.MemoryContext, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(&got, want) || !reflect.DeepEqual(taskToResponse(row, testWorkspaceID).MemoryContext, want) {
					t.Fatalf("persisted context drift: %+v != %+v", got, want)
				}
			}
			assertStored(receipt)
			var hash string
			dbfx.QueryRow(t, `SELECT token_hash FROM task_token WHERE task_id=$1 LIMIT 1`, taskID).Scan(&hash)
			token := db.CreateTaskTokenParams{TokenHash: hash, TaskID: stored.ID, AgentID: stored.AgentID, WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}}
			replacement := &service.TaskMemoryContext{AgentStatus: "unavailable", AgentVersions: []service.MemoryVersion{}}
			// Duplicate credential insertion fails AFTER the context write, which must roll back.
			if _, err = testHandler.TaskService.FinalizeTaskClaim(ctx, stored, token, nil, false, replacement); err == nil {
				t.Fatal("duplicate token unexpectedly accepted")
			}
			assertStored(receipt)
			dbfx.Exec(t, `UPDATE agent_memory SET status='rejected',revision=revision+1 WHERE agent_id=$1`, agent)
			dbfx.Exec(t, `UPDATE project SET memory_expires_at=now()-interval '1 hour' WHERE id=$1`, project)
			if _, err = testHandler.TaskService.RequeueTaskAfterClaimFailure(ctx, stored); err != nil {
				t.Fatal(err)
			}
			second := claim()
			if second.MemoryContext == nil || second.MemoryContext.AgentStatus != "loaded" || len(second.MemoryContext.AgentVersions) != 0 || second.MemoryContext.ProjectVersion != nil || second.MemoryContext.DispatchedAt == receipt.DispatchedAt {
				t.Fatalf("reclaim retained obsolete memory: %+v", second.MemoryContext)
			}
			assertStored(second.MemoryContext)
			token.TokenHash = "stale-memory-context-" + taskID
			if _, err = testHandler.TaskService.FinalizeTaskClaim(ctx, stored, token, nil, false, receipt); err == nil {
				t.Fatal("stale claim finalized")
			}
			assertStored(second.MemoryContext)
			latestClaim, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = testHandler.TaskService.RequeueTaskAfterClaimFailure(ctx, latestClaim); err != nil {
				t.Fatal(err)
			}
			originalQueries := testHandler.TaskService.Queries
			t.Cleanup(func() { testHandler.TaskService.Queries = originalQueries })
			testHandler.TaskService.Queries = db.New(unavailableMemoryDB{testPool})
			unavailable := claim()
			testHandler.TaskService.Queries = originalQueries
			if unavailable.MemoryContext == nil || unavailable.MemoryContext.AgentStatus != "unavailable" || len(unavailable.MemoryContext.AgentVersions) != 0 || len(unavailable.Agent.Memories) != 0 {
				t.Fatalf("failed load became empty success: %+v", unavailable.MemoryContext)
			}
			second = unavailable
			assertStored(second.MemoryContext)
			latest, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1`, taskID)
			if _, err = testHandler.TaskService.FinalizeTaskClaim(ctx, latest, token, nil, false, receipt); err == nil {
				t.Fatal("started run context overwritten")
			}
			assertStored(second.MemoryContext)
			var count int
			dbfx.QueryRow(t, `SELECT count(*) FROM task_token WHERE token_hash=$1`, token.TokenHash).Scan(&count)
			if count != 0 {
				t.Fatal("stale/started claim leaked credentials")
			}
		})
	}
}
