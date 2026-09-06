package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F09: the canonical matrix for "may this run be reverted to". The endpoint
// suite in worktree_revert_test.go keeps the wiring and the refusal statuses;
// this file owns the predicate.
func TestTaskRevertable(t *testing.T) {
	sha := pgtype.Text{String: "aaaa1111bbbb2222", Valid: true}
	cases := []struct {
		name       string
		task       db.AgentTaskQueue
		revertable bool
		why        string
	}{
		{"completed with a checkpoint", db.AgentTaskQueue{Status: "completed", CheckpointSha: sha}, true,
			"the ordinary case"},
		{"failed with a checkpoint", db.AgentTaskQueue{Status: "failed", CheckpointSha: sha}, true,
			"a run that died partway is exactly the one a user rolls back; Finalize recorded its checkpoint too"},
		{"cancelled with a checkpoint", db.AgentTaskQueue{Status: "cancelled", CheckpointSha: sha}, true,
			"a cancelled worktree run has already committed what the agent left"},
		{"no checkpoint", db.AgentTaskQueue{Status: "completed"}, false,
			"nothing to reset the branch to"},
		{"blank checkpoint", db.AgentTaskQueue{Status: "completed", CheckpointSha: pgtype.Text{String: "  ", Valid: true}}, false,
			"a whitespace value is not a commit"},
		{"still running", db.AgentTaskQueue{Status: "running", CheckpointSha: sha}, false,
			"the checkpoint belongs to an earlier attempt of the same id, and Finalize is about to move the branch again"},
		{"queued", db.AgentTaskQueue{Status: "queued", CheckpointSha: sha}, false,
			"same reason as running"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskRevertable(tc.task); got != tc.revertable {
				t.Fatalf("taskRevertable(status=%q, checkpoint=%q) = %v, want %v — %s",
					tc.task.Status, tc.task.CheckpointSha.String, got, tc.revertable, tc.why)
			}
		})
	}
}
