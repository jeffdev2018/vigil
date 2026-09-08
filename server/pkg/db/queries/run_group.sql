-- Racing attempts on one issue (F11 / JEF-6).
--
-- The group is bookkeeping: which attempts belong together, which one the user
-- kept, whether the race is still open. The attempts themselves are ordinary
-- agent_task_queue rows and every query about them lives in agent.sql.

-- name: CreateRunGroup :one
INSERT INTO run_group (id, workspace_id, issue_id, created_by, attempt_count)
VALUES ($1, $2, $3, sqlc.narg('created_by')::uuid, $4)
RETURNING *;

-- name: GetRunGroup :one
SELECT * FROM run_group WHERE id = $1;

-- name: GetRunGroupInWorkspace :one
-- Tenant guard for the group-scoped endpoints: the group is only reachable
-- from the workspace that owns it, whatever id the caller puts in the path.
SELECT * FROM run_group WHERE id = $1 AND workspace_id = $2;

-- name: ListRunGroupsForIssue :many
SELECT * FROM run_group WHERE issue_id = $1 ORDER BY created_at DESC;

-- name: SettleRunGroup :one
-- Designates the winner. The CAS on status is what makes selection idempotent
-- under a double-click: the second call finds no 'running' row and errors,
-- rather than re-running the losing-branch deletion against a group whose
-- branches are already gone.
UPDATE run_group
SET status = 'settled', winner_task_id = $2, settled_at = now()
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: AbandonRunGroup :one
-- Same CAS, and deliberately no winner: an abandoned group kept nothing.
UPDATE run_group
SET status = 'abandoned', settled_at = now()
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: ListRunGroupTasks :many
-- The attempts of one group, oldest first so the columns in the compare view
-- keep the order the user composed them in.
SELECT * FROM agent_task_queue WHERE run_group_id = $1 ORDER BY created_at ASC, id ASC;

-- name: ListRunGroupTasksForIssue :many
-- Every attempt of every group on one issue, for the list endpoint: one read
-- instead of one per group.
SELECT * FROM agent_task_queue
WHERE run_group_id IS NOT NULL AND issue_id = $1
ORDER BY created_at ASC, id ASC;

-- name: RecordTaskDiff :one
-- Stores what the daemon computed at Finalize. diff_unified is NULL when the
-- run exceeded the byte bound; diff_stat is stored either way, so "truncated"
-- is exactly "there is a stat and no unified diff".
UPDATE agent_task_queue
SET diff_stat = sqlc.narg('diff_stat')::jsonb,
    diff_unified = NULLIF(COALESCE(sqlc.narg('diff_unified')::text, ''), '')
WHERE id = $1
RETURNING *;

-- name: CountActiveRunGroupsForIssue :one
-- One open race per issue at a time. A second concurrent group would put two
-- sets of attempts of possibly the same agents on one issue, and the losing
-- side of one race could delete a branch the other race is still writing to.
SELECT count(*) FROM run_group WHERE issue_id = $1 AND status = 'running';

-- name: PurgeWorkspaceRunGroups :exec
-- Workspace teardown. Attempts themselves live on agent_task_queue and are
-- purged by the task sweep; this drops the group rows that scoped them.
DELETE FROM run_group WHERE workspace_id = $1;
