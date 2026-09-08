package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type runtimeClaimAccessFixture struct {
	pool      *pgxpool.Pool
	agentID   pgtype.UUID
	runtimeID pgtype.UUID
	taskID    string
}

func newRuntimeClaimAccessFixture(
	t *testing.T,
	visibility string,
	sameOwner bool,
	matchingBinding bool,
	taskStatus string,
) runtimeClaimAccessFixture {
	t.Helper()

	pool := newResolveOriginatorPool(t)
	bootstrap := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	runtimeOwnerID := bootstrap.User(t,
		fmt.Sprintf("runtime-owner-%d", suffix),
		fmt.Sprintf("runtime-owner-%d@example.com", suffix),
	)
	agentOwnerID := runtimeOwnerID
	if !sameOwner {
		agentOwnerID = bootstrap.User(t,
			fmt.Sprintf("agent-owner-%d", suffix),
			fmt.Sprintf("agent-owner-%d@example.com", suffix),
		)
	}
	workspaceID := bootstrap.Workspace(t,
		fmt.Sprintf("runtime-claim-access-%d", suffix),
		fmt.Sprintf("runtime-claim-access-%d", suffix),
	)

	fx := testutil.New(pool, workspaceID, runtimeOwnerID)
	fx.Member(t, workspaceID, runtimeOwnerID, "owner")
	if agentOwnerID != runtimeOwnerID {
		fx.Member(t, workspaceID, agentOwnerID, "member")
	}
	taskRuntimeID := fx.Runtime(t, "task-runtime", testutil.Cols{
		"visibility": visibility,
		"owner_id":   runtimeOwnerID,
	})
	boundRuntimeID := taskRuntimeID
	if !matchingBinding {
		boundRuntimeID = fx.Runtime(t, "agent-runtime", testutil.Cols{
			"visibility": "public",
			"owner_id":   runtimeOwnerID,
		})
	}
	agentID := fx.Agent(t, "claim-agent", boundRuntimeID, testutil.Cols{
		"owner_id":             agentOwnerID,
		"max_concurrent_tasks": 5,
	})
	issueID := fx.Issue(t, "runtime claim access")
	taskCols := testutil.Cols{
		"runtime_id": taskRuntimeID,
		"issue_id":   issueID,
		"status":     taskStatus,
	}
	if taskStatus == "dispatched" {
		taskCols["dispatched_at"] = testutil.Raw("now() - interval '10 minutes'")
		taskCols["prepare_lease_expires_at"] = testutil.Raw("now() - interval '1 minute'")
	}
	taskID := fx.Task(t, agentID, taskCols)

	return runtimeClaimAccessFixture{
		pool:      pool,
		agentID:   util.MustParseUUID(agentID),
		runtimeID: util.MustParseUUID(taskRuntimeID),
		taskID:    taskID,
	}
}

func TestRuntimeAccessGatesQueuedTaskClaims(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		visibility      string
		sameOwner       bool
		matchingBinding bool
		ownerlessAgent  bool
		wantClaim       bool
	}{
		{name: "private runtime rejects foreign agent", visibility: "private", wantClaim: false, matchingBinding: true},
		{name: "private runtime accepts owner agent", visibility: "private", sameOwner: true, matchingBinding: true, wantClaim: true},
		{name: "private runtime routes ownerless agent to handler", visibility: "private", sameOwner: true, matchingBinding: true, ownerlessAgent: true, wantClaim: true},
		{name: "public runtime accepts foreign agent", visibility: "public", wantClaim: true, matchingBinding: true},
		{name: "task runtime must match agent binding", visibility: "public", wantClaim: false, matchingBinding: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRuntimeClaimAccessFixture(t, tt.visibility, tt.sameOwner, tt.matchingBinding, "queued")
			if tt.ownerlessAgent {
				if _, err := fixture.pool.Exec(ctx, `UPDATE agent SET owner_id = NULL WHERE id = $1`, fixture.agentID); err != nil {
					t.Fatalf("clear agent owner: %v", err)
				}
			}
			q := db.New(fixture.pool)

			candidates, err := q.ListQueuedClaimCandidatesByRuntime(ctx, fixture.runtimeID)
			if err != nil {
				t.Fatalf("list singular candidates: %v", err)
			}
			batchCandidates, err := q.ListQueuedClaimCandidatesByRuntimes(ctx, []pgtype.UUID{fixture.runtimeID})
			if err != nil {
				t.Fatalf("list batch candidates: %v", err)
			}
			wantCandidates := 0
			if tt.wantClaim {
				wantCandidates = 1
			}
			if len(candidates) != wantCandidates {
				t.Fatalf("singular candidates = %d, want %d", len(candidates), wantCandidates)
			}
			if len(batchCandidates) != wantCandidates {
				t.Fatalf("batch candidates = %d, want %d", len(batchCandidates), wantCandidates)
			}

			claimed, err := q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
				AgentID:          fixture.agentID,
				RuntimeID:        fixture.runtimeID,
				PrepareLeaseSecs: 60,
				RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
			})
			if !tt.wantClaim {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("claim error = %v, want no rows", err)
				}
				var status string
				if err := fixture.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&status); err != nil {
					t.Fatalf("read task status: %v", err)
				}
				if status != "queued" {
					t.Fatalf("task status = %q, want queued", status)
				}
				return
			}
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			if util.UUIDToString(claimed.ID) != fixture.taskID {
				t.Fatalf("claimed task = %s, want %s", util.UUIDToString(claimed.ID), fixture.taskID)
			}
		})
	}
}

func TestRuntimeAccessGatesStaleDispatchReclaim(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		visibility      string
		sameOwner       bool
		matchingBinding bool
		ownerlessAgent  bool
		wantReclaim     bool
	}{
		{name: "private runtime rejects foreign agent", visibility: "private", wantReclaim: false, matchingBinding: true},
		{name: "private runtime accepts owner agent", visibility: "private", sameOwner: true, matchingBinding: true, wantReclaim: true},
		{name: "private runtime routes ownerless agent to handler", visibility: "private", sameOwner: true, matchingBinding: true, ownerlessAgent: true, wantReclaim: true},
		{name: "public runtime accepts foreign agent", visibility: "public", wantReclaim: true, matchingBinding: true},
		{name: "task runtime must match agent binding", visibility: "public", wantReclaim: false, matchingBinding: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRuntimeClaimAccessFixture(t, tt.visibility, tt.sameOwner, tt.matchingBinding, "dispatched")
			if tt.ownerlessAgent {
				if _, err := fixture.pool.Exec(ctx, `UPDATE agent SET owner_id = NULL WHERE id = $1`, fixture.agentID); err != nil {
					t.Fatalf("clear agent owner: %v", err)
				}
			}
			q := db.New(fixture.pool)
			params := db.ReclaimStaleDispatchedTaskForRuntimeParams{
				RuntimeID:         fixture.runtimeID,
				PrepareLeaseSecs:  60,
				ClaimRecoverySecs: 30,
				RuntimeStaleSecs:  RuntimeClaimFreshnessSeconds,
			}
			reclaimed, err := q.ReclaimStaleDispatchedTaskForRuntime(ctx, params)
			if !tt.wantReclaim {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("singular reclaim error = %v, want no rows", err)
				}
			} else {
				if err != nil {
					t.Fatalf("singular reclaim: %v", err)
				}
				if util.UUIDToString(reclaimed.ID) != fixture.taskID {
					t.Fatalf("singular reclaimed task = %s, want %s", util.UUIDToString(reclaimed.ID), fixture.taskID)
				}
			}

			// Restore the stale generation so the batch path evaluates the same gate.
			if _, err := fixture.pool.Exec(ctx, `
				UPDATE agent_task_queue
				SET dispatched_at = now() - interval '10 minutes',
				    prepare_lease_expires_at = now() - interval '1 minute'
				WHERE id = $1
			`, fixture.taskID); err != nil {
				t.Fatalf("reset stale dispatch: %v", err)
			}
			batch, err := q.ReclaimStaleDispatchedTasksForRuntimes(ctx, db.ReclaimStaleDispatchedTasksForRuntimesParams{
				RuntimeIds:        []pgtype.UUID{fixture.runtimeID},
				PrepareLeaseSecs:  60,
				ClaimRecoverySecs: 30,
				RuntimeStaleSecs:  RuntimeClaimFreshnessSeconds,
				MaxTasks:          10,
			})
			if err != nil {
				t.Fatalf("batch reclaim: %v", err)
			}
			wantBatch := 0
			if tt.wantReclaim {
				wantBatch = 1
			}
			if len(batch) != wantBatch {
				t.Fatalf("batch reclaimed %d tasks, want %d", len(batch), wantBatch)
			}
		})
	}
}

func TestClaimTaskRejectsMismatchedAgentRuntime(t *testing.T) {
	ctx := context.Background()
	fixture := newRuntimeClaimAccessFixture(t, "public", false, false, "queued")
	svc := NewTaskService(db.New(fixture.pool), fixture.pool, nil, events.New())

	claimed, err := svc.claimTask(ctx, fixture.agentID, fixture.runtimeID, true)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed != nil {
		t.Fatalf("claimed mismatched task %s", util.UUIDToString(claimed.ID))
	}

	// The runtimePinned flag (JEF-276) relaxes only the cheap Go pre-filter.
	// This task is not a benchmark leg, so ClaimAgentTask's own fence still
	// refuses it — the flag can never dispatch what the SQL would reject.
	pinned, err := svc.claimTask(ctx, fixture.agentID, fixture.runtimeID, true)
	if err != nil {
		t.Fatalf("claim task with the pin flag: %v", err)
	}
	if pinned != nil {
		t.Fatalf("the SQL fence let a mismatched non-benchmark task through: %s", util.UUIDToString(pinned.ID))
	}

	var status string
	if err := fixture.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %q, want queued", status)
	}
}

func TestClaimTaskUsesCurrentAgentRuntimeWhenRuntimeIDIsOmitted(t *testing.T) {
	ctx := context.Background()
	fixture := newRuntimeClaimAccessFixture(t, "public", true, true, "queued")
	svc := NewTaskService(db.New(fixture.pool), fixture.pool, nil, events.New())

	claimed, err := svc.ClaimTask(ctx, fixture.agentID)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed == nil {
		t.Fatal("ClaimTask returned nil, want task from the agent's current runtime")
	}
	if util.UUIDToString(claimed.ID) != fixture.taskID {
		t.Fatalf("claimed task = %s, want %s", util.UUIDToString(claimed.ID), fixture.taskID)
	}
}

// A runtime pool failover (K28) moves a task to another runtime the owner
// listed in the agent's pool, deliberately different from the agent's binding.
// Without the fence exemption the moved task is claimable by nobody: the target
// runtime's fence fails the agent-binding check and the agent's own runtime
// never sees the row. Membership is read per row, so dropping the runtime from
// the pool closes the exemption again.
func TestClaimTaskHonorsRuntimePoolMembership(t *testing.T) {
	ctx := context.Background()
	fixture := newRuntimeClaimAccessFixture(t, "public", true, false, "queued")
	q := db.New(fixture.pool)
	svc := NewTaskService(q, fixture.pool, nil, events.New())

	// Control: without a pool the mismatched task stays unclaimable (the fence
	// TestClaimTaskRejectsMismatchedAgentRuntime covers from the other side).
	refused, err := svc.claimTask(ctx, fixture.agentID, fixture.runtimeID, false)
	if err != nil {
		t.Fatalf("claim mismatched task: %v", err)
	}
	if refused != nil {
		t.Fatal("the fence let a plain mismatched task through")
	}

	var poolID pgtype.UUID
	if err := fixture.pool.QueryRow(ctx, `
		INSERT INTO runtime_pool (workspace_id, name, runtime_ids)
		SELECT workspace_id, 'claim-pool', jsonb_build_array($2::text) FROM agent WHERE id = $1
		RETURNING id`, fixture.agentID, util.UUIDToString(fixture.runtimeID)).Scan(&poolID); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() { fixture.pool.Exec(context.Background(), `DELETE FROM runtime_pool WHERE id = $1`, poolID) })
	if _, err := fixture.pool.Exec(ctx, `UPDATE agent SET runtime_pool_id = $2 WHERE id = $1`, fixture.agentID, poolID); err != nil {
		t.Fatalf("attach pool: %v", err)
	}
	// The failover marker the K28 mover writes; the cheap Go pre-filter keys on it.
	if _, err := fixture.pool.Exec(ctx, `UPDATE agent_task_queue SET failover_history = '[{"reason":"runtime_offline"}]'::jsonb WHERE id = $1`, fixture.taskID); err != nil {
		t.Fatalf("stamp failover: %v", err)
	}

	candidates, err := q.ListQueuedClaimCandidatesByRuntime(ctx, fixture.runtimeID)
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	found := false
	for _, c := range candidates {
		if util.UUIDToString(c.ID) == fixture.taskID {
			found = true
		}
	}
	if !found {
		t.Fatal("the pool-moved task is missing from the runtime's candidate list")
	}

	claimed, err := svc.claimTask(ctx, fixture.agentID, fixture.runtimeID, true)
	if err != nil {
		t.Fatalf("claim pool-moved task: %v", err)
	}
	if claimed == nil {
		t.Fatal("the fence refused a runtime the owner listed in the agent's pool — the task is claimable by nobody")
	}
	if util.UUIDToString(claimed.ID) != fixture.taskID {
		t.Fatalf("claimed task = %s, want %s", util.UUIDToString(claimed.ID), fixture.taskID)
	}
}
