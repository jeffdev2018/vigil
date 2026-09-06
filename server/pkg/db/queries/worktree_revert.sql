-- name: CreateWorktreeRevertRequest :one
-- F09: enqueue one revert. The unique partial index on (target_task_id) WHERE
-- status='pending' is what makes the caller's "is one already in flight" check
-- safe across nodes — a losing INSERT surfaces as a unique violation the
-- handler turns into 409.
INSERT INTO worktree_revert_request (
    id, workspace_id, runtime_id, target_task_id, issue_id, chat_session_id, requested_by
) VALUES (
    sqlc.arg('id'), sqlc.arg('workspace_id'), sqlc.arg('runtime_id'), sqlc.arg('target_task_id'),
    sqlc.narg('issue_id'), sqlc.narg('chat_session_id'), sqlc.arg('requested_by')
)
RETURNING *;

-- name: GetPendingWorktreeRevertForConversation :one
-- F09: the in-flight revert on this conversation, if any. 'claimed' counts as
-- in flight: the daemon is already moving refs, and a second request would
-- race it on the same branch.
SELECT * FROM worktree_revert_request
WHERE status IN ('pending', 'claimed')
  AND (
      (sqlc.narg('issue_id')::uuid IS NOT NULL AND issue_id = sqlc.narg('issue_id')::uuid)
      OR (sqlc.narg('chat_session_id')::uuid IS NOT NULL AND chat_session_id = sqlc.narg('chat_session_id')::uuid)
  )
ORDER BY created_at DESC
LIMIT 1;

-- name: HasPendingWorktreeRevert :one
-- F09: the heartbeat probe. Hits idx_worktree_revert_request_pending only.
SELECT EXISTS (
    SELECT 1 FROM worktree_revert_request
    WHERE runtime_id = sqlc.arg('runtime_id') AND status = 'pending'
);

-- name: ClaimWorktreeRevertRequest :one
-- F09: hand the oldest pending revert for this runtime to the daemon, exactly
-- once. The claim is the same UPDATE that reads it, so two heartbeats from two
-- API nodes cannot both dispatch the same request.
UPDATE worktree_revert_request
SET status = 'claimed', claimed_at = now()
WHERE worktree_revert_request.id = (
    SELECT r.id FROM worktree_revert_request r
    WHERE r.runtime_id = sqlc.arg('runtime_id') AND r.status = 'pending'
    ORDER BY r.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: GetWorktreeRevertRequest :one
SELECT * FROM worktree_revert_request WHERE id = sqlc.arg('id');

-- name: SettleWorktreeRevertRequest :one
-- F09: record the outcome. Only a claimed request settles, so a retried report
-- after the first one landed changes nothing and returns no row — which is what
-- makes the daemon's retry schedule safe to run against a destructive action.
UPDATE worktree_revert_request
SET status = sqlc.arg('status'),
    error = sqlc.narg('error'),
    settled_at = now()
WHERE id = sqlc.arg('id') AND status = 'claimed'
RETURNING *;

-- name: ReleaseStaleWorktreeRevertClaims :many
-- F09: a daemon that died holding a claim would otherwise block the
-- conversation forever, because the 409 check counts 'claimed' as in flight.
-- Put those back to pending so the next heartbeat picks them up.
UPDATE worktree_revert_request
SET status = 'pending', claimed_at = NULL
WHERE status = 'claimed' AND claimed_at < now() - sqlc.arg('stale_after')::interval
RETURNING *;

-- name: DeleteWorkspaceWorktreeRevertRequests :exec
-- F09: workspace teardown. The requests reference runs and issues that are
-- being deleted in the same transaction; nothing outside the workspace reads
-- them, so they go with it.
DELETE FROM worktree_revert_request WHERE workspace_id = sqlc.arg('workspace_id');
