package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Per-leg accounting (JEF-274). This file is the canonical layer for the two
// pure decisions — which run a leg points at, and whether a retry reads as a
// retry or a fallback — plus one end-to-end check that FailTask's retry child
// actually carries them.

func TestWorkflowRoot(t *testing.T) {
	root := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	parent := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	// A parent that already belongs to a workflow passes its root down, so a
	// chain (draft → review → revision → retry) stays one flat workflow
	// instead of a linked list nobody can total in one query.
	if got := WorkflowRoot(db.AgentTaskQueue{ID: parent, WorkflowRootTaskID: root}); got != root {
		t.Errorf("nested leg root = %v, want the parent's root %v", got, root)
	}
	// A parent that is not itself a leg IS the root.
	if got := WorkflowRoot(db.AgentTaskQueue{ID: parent}); got != parent {
		t.Errorf("first leg root = %v, want the parent %v", got, parent)
	}
	// No parent at all (duel candidate, fan-out synthesis, campaign shard):
	// an invalid UUID, which stamps NULL and leaves the leg its own root.
	if WorkflowRoot(db.AgentTaskQueue{}).Valid {
		t.Error("a leg with no parent must stamp a NULL root")
	}
}

func TestRetryLegRole(t *testing.T) {
	cases := []struct {
		name    string
		history []byte
		want    string
	}{
		{"re-run where it failed", nil, LegRoleRetry},
		{"empty history", []byte(`[]`), LegRoleRetry},
		{"unreadable history", []byte(`not json`), LegRoleRetry},
		{"moved to another runtime", []byte(`[{"from_runtime_id":"a","to_runtime_id":"b","reason":"offline"}]`), LegRoleFallback},
	}
	for _, tc := range cases {
		if got := RetryLegRole(tc.history); got != tc.want {
			t.Errorf("%s: role = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestFailTaskStampsRetryLeg: the retry child FailTask creates in its own
// transaction is stamped as a leg of the run it re-attempts, so the failed
// attempt and the retry total together instead of reading as two unrelated
// runs on the issue.
func TestFailTaskStampsRetryLeg(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts)
		VALUES ($1, $2, $3, 'running', 0, 1, 3)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert parent task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
	})

	svc := NewTaskService(q, pool, nil, events.New())
	if _, err := svc.FailTask(ctx, parentID, "the run timed out", "", "", "", "timeout", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var legRole string
	var root pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT leg_role, workflow_root_task_id FROM agent_task_queue WHERE parent_task_id = $1
	`, parentID).Scan(&legRole, &root); err != nil {
		t.Fatalf("read retry child: %v", err)
	}
	if legRole != LegRoleRetry {
		t.Errorf("leg_role = %q, want %q", legRole, LegRoleRetry)
	}
	if root != parentID {
		t.Errorf("workflow_root_task_id = %v, want the failed run %v", root, parentID)
	}
}
