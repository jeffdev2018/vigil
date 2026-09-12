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

-- name: SetRunGroupJudgement :one
-- Stores the LLM judge's verdict (JEF-234 follow-up). No CAS: judging changes
-- nothing about the race itself, so a re-judge simply overwrites the previous
-- judgement — including a stored 'failed' one.
UPDATE run_group
SET judgement = $2
WHERE id = $1
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

-- name: ListRunGroupAttemptMetricsForIssue :many
-- Per-attempt comparison metrics (JEF-234) for every run-group attempt on one
-- issue: which runtime ran it (custom_name wins for display, as in the runtime
-- API), what it cost, how long it took. Keyed by task_id so the handler folds
-- rows into the attempts it already loaded; one read for every group of the
-- issue instead of one per group.
--
-- cost_usd_ticks sums task_usage per attempt; a row that reported no provider
-- price contributes NULL, and SUM over NULLs is NULL, so COALESCE pins the
-- "no usage reported" case to 0. duration_seconds is 0 while the attempt has
-- not completed: a running attempt has no finite duration, and the queue wait
-- before started_at only counts once the run is done.
SELECT
    atq.id AS task_id,
    atq.runtime_id,
    COALESCE(NULLIF(r.custom_name, ''), r.name, '') AS runtime_name,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
    (CASE WHEN atq.completed_at IS NOT NULL
          THEN GREATEST(EXTRACT(EPOCH FROM (atq.completed_at - COALESCE(atq.started_at, atq.created_at)))::bigint, 0)
          ELSE 0 END)::bigint AS duration_seconds
FROM agent_task_queue atq
LEFT JOIN agent_runtime r ON r.id = atq.runtime_id
LEFT JOIN task_usage tu ON tu.task_id = atq.id
WHERE atq.run_group_id IS NOT NULL AND atq.issue_id = $1
GROUP BY atq.id, atq.runtime_id, r.name, r.custom_name
ORDER BY atq.created_at ASC, atq.id ASC;
