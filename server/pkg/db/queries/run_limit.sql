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
SELECT t.* FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE t.status = 'running' AND t.started_at IS NOT NULL
  AND EXISTS (SELECT 1 FROM run_limit_policy p WHERE p.workspace_id = a.workspace_id)
ORDER BY t.started_at
LIMIT $1;

-- name: PurgeWorkspaceRunLimits :exec
DELETE FROM run_limit_policy WHERE workspace_id = $1;

-- name: PurgeWorkspaceRunLimitEvents :exec
DELETE FROM run_limit_event WHERE workspace_id = $1;
