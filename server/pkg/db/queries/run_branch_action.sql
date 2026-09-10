-- Promote / discard requests for the branch a terminal run delivered (JEF-255).
--
-- Same lifecycle as worktree_revert.sql (F09): a durable queue the daemon
-- drains through its heartbeat, because the branch lives on the daemon's
-- machine and only that machine can push or delete it.

-- name: CreateRunBranchActionRequest :one
-- Enqueue one action. The endpoint refuses a second in-flight request for the
-- same task with 409 before inserting; there is deliberately no unique index
-- to race here because the promote/discard CAS on the task row (promoted_at /
-- discarded_at) is the final arbiter of "already done", and a settled request
-- must never block a retry of a failed one.
INSERT INTO run_branch_action_request (
    workspace_id, task_id, runtime_id, action, branch_name, base_branch, created_by
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('task_id'), sqlc.arg('runtime_id'),
    sqlc.arg('action'), sqlc.arg('branch_name'), sqlc.arg('base_branch'),
    sqlc.narg('created_by')
)
RETURNING *;

-- name: GetRunBranchActionRequest :one
SELECT * FROM run_branch_action_request WHERE id = sqlc.arg('id');

-- name: GetPendingRunBranchActionForTask :one
-- The in-flight action on this run, if any. 'claimed' counts as in flight: the
-- daemon is already pushing or deleting, and a second request would race it on
-- the same branch.
SELECT * FROM run_branch_action_request
WHERE task_id = sqlc.arg('task_id') AND status IN ('pending', 'claimed')
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPendingRunBranchActionsForIssue :many
-- One batched read for the issue's run list, so pending_branch_action on every
-- AgentTaskResponse costs one query for the issue instead of one per run.
SELECT r.* FROM run_branch_action_request r
JOIN agent_task_queue t ON t.id = r.task_id
WHERE t.issue_id = sqlc.arg('issue_id') AND r.status IN ('pending', 'claimed')
ORDER BY r.created_at ASC;

-- name: HasPendingRunBranchAction :one
-- The heartbeat probe. Hits idx_run_branch_action_request_pending only.
SELECT EXISTS (
    SELECT 1 FROM run_branch_action_request
    WHERE runtime_id = sqlc.arg('runtime_id') AND status = 'pending'
);

-- name: ClaimRunBranchActionRequest :one
-- Hand the oldest pending action for this runtime to the daemon, exactly once.
-- The claim is the same UPDATE that reads it, so two heartbeats from two API
-- nodes cannot both dispatch the same request.
UPDATE run_branch_action_request
SET status = 'claimed', claimed_at = now(), updated_at = now()
WHERE run_branch_action_request.id = (
    SELECT r.id FROM run_branch_action_request r
    WHERE r.runtime_id = sqlc.arg('runtime_id') AND r.status = 'pending'
    ORDER BY r.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: SettleRunBranchActionRequest :one
-- Record the outcome. Only a claimed request settles, so a retried report
-- after the first one landed changes nothing and returns no row — which is
-- what makes the daemon's retry schedule safe to run against a destructive
-- action.
UPDATE run_branch_action_request
SET status = sqlc.arg('status'),
    pr_url = COALESCE(NULLIF(sqlc.narg('pr_url')::text, ''), pr_url),
    error = COALESCE(sqlc.narg('error')::text, ''),
    completed_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id') AND status = 'claimed'
RETURNING *;

-- name: ReleaseStaleRunBranchActionClaims :many
-- A daemon that died holding a claim would otherwise block the run's actions
-- forever, because the 409 check counts 'claimed' as in flight. Put those back
-- to pending so the next heartbeat picks them up.
UPDATE run_branch_action_request
SET status = 'pending', claimed_at = NULL, updated_at = now()
WHERE status = 'claimed' AND claimed_at < now() - sqlc.arg('stale_after')::interval
RETURNING *;

-- name: MarkTaskPromoted :one
-- The run-side fact of a completed promote. The CAS is what makes the action
-- safe to retry: a discarded run, an already-promoted run, or a retried report
-- matches no row, and the caller turns that into "not promotable" / a no-op.
UPDATE agent_task_queue
SET promoted_at = now(), promote_pr_url = sqlc.arg('promote_pr_url')
WHERE id = sqlc.arg('id') AND promoted_at IS NULL AND discarded_at IS NULL
RETURNING *;

-- name: MarkTaskDiscarded :one
-- Same CAS, other direction. A discarded run may never be promoted afterwards:
-- its branch is gone.
UPDATE agent_task_queue
SET discarded_at = now()
WHERE id = sqlc.arg('id') AND discarded_at IS NULL AND promoted_at IS NULL
RETURNING *;

-- name: DeleteWorkspaceRunBranchActionRequests :exec
-- Workspace teardown. The requests reference runs and runtimes that are being
-- deleted in the same transaction; nothing outside the workspace reads them,
-- so they go with it.
DELETE FROM run_branch_action_request WHERE workspace_id = sqlc.arg('workspace_id');
