-- Run limits (K03).

-- name: CreateRunLimitPolicy :one
INSERT INTO run_limit_policy (id, workspace_id, scope_type, scope_id, max_cost_usd_ticks, max_duration_seconds, max_turns, max_tool_calls, warn_bps, action, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateRunLimitPolicy :one
UPDATE run_limit_policy
SET max_cost_usd_ticks = $2, max_duration_seconds = $3, max_turns = $4, max_tool_calls = $5, warn_bps = $6, action = $7, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetRunLimitPolicy :one
SELECT * FROM run_limit_policy WHERE id = $1;

-- name: ListRunLimitPolicies :many
SELECT * FROM run_limit_policy WHERE workspace_id = $1 ORDER BY scope_type, created_at;

-- name: DeleteRunLimitPolicy :exec
DELETE FROM run_limit_policy WHERE id = $1;

-- name: ListRunLimitPoliciesForRun :many
-- Every policy that applies to one run: the workspace one, the project one, the agent one.
SELECT * FROM run_limit_policy
WHERE workspace_id = $1
  AND ((scope_type = 'workspace') OR (scope_type = 'project' AND scope_id = sqlc.narg('project_id')) OR (scope_type = 'agent' AND scope_id = $2));

-- name: SumTaskCostTicks :one
SELECT COALESCE(SUM(cost_usd_ticks), 0)::bigint FROM task_usage WHERE task_id = $1;

-- name: CountTaskMessagesByType :many
SELECT type, COUNT(*)::bigint AS n FROM task_message WHERE task_id = $1 GROUP BY type;

-- name: CreateRunLimitEvent :one
INSERT INTO run_limit_event (id, workspace_id, task_id, policy_id, gate, level, observed, limit_value)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListRunLimitEvents :many
SELECT * FROM run_limit_event WHERE task_id = $1 ORDER BY created_at;

-- name: ListRunLimitEventsForIssue :many
SELECT e.* FROM run_limit_event e JOIN agent_task_queue t ON t.id = e.task_id WHERE t.issue_id = $1 ORDER BY e.created_at DESC LIMIT 50;

-- name: ListRunningTasksForLimits :many
-- Every running run, not only those in a workspace that configured a policy.
-- The duration gate is the one cap that can only move with the clock, so it
-- fires here or nowhere; cost, turns and tool calls are also evaluated when the
-- run reports usage. Filtering on an existing run_limit_policy row used to be
-- correct, because a workspace without one had no caps at all — since the
-- built-in wall (EffectiveRunLimits) it is not, and it left the default
-- duration cap unable to fire for exactly the workspaces that never configured
-- anything.
SELECT t.* FROM agent_task_queue t
WHERE t.status = 'running' AND t.started_at IS NOT NULL
ORDER BY t.started_at
LIMIT $1;

-- name: PurgeWorkspaceRunLimits :exec
DELETE FROM run_limit_policy WHERE workspace_id = $1;

-- name: PurgeWorkspaceRunLimitEvents :exec
DELETE FROM run_limit_event WHERE workspace_id = $1;

-- name: ListTasksOnTerminalIssues :many
-- Runs whose issue has been closed or cancelled under them. The status is
-- returned rather than filtered in SQL because a workspace can define its own
-- statuses: only issuestatus.Effective knows which category a custom key maps
-- to, so the decision belongs in Go and this query only narrows the candidates.
SELECT t.id, t.agent_id, t.issue_id, i.workspace_id, i.status AS issue_status
FROM agent_task_queue t
JOIN issue i ON i.id = t.issue_id
WHERE t.status IN ('queued', 'running')
  AND i.status IS NOT NULL AND i.status <> ''
ORDER BY t.created_at
LIMIT $1;

-- name: EndTasksOnTerminalIssues :many
-- Ends the runs the caller judged orphaned. The terminal decision is made in
-- Go (issuestatus.Effective, because a workspace can name its own statuses)
-- and only the ids arrive here, so this is the write half of a two-step sweep
-- and re-checks the status it is allowed to end from.
UPDATE agent_task_queue
SET status = 'failed',
    completed_at = now(),
    error = 'The issue was closed while this run was still going, so there is nothing left for it to deliver.',
    failure_reason = 'issue_terminal',
    prepare_lease_expires_at = NULL
WHERE id = ANY(sqlc.arg('ids')::uuid[])
  AND status IN ('queued', 'running')
RETURNING id;
