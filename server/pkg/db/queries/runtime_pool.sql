-- Runtime pools (K28).

-- name: CreateRuntimePool :one
INSERT INTO runtime_pool (id, workspace_id, name, runtime_ids, degraded_runtime_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListRuntimePools :many
SELECT * FROM runtime_pool WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: GetRuntimePool :one
SELECT * FROM runtime_pool WHERE id = $1;

-- name: UpdateRuntimePool :one
UPDATE runtime_pool SET name = $2, runtime_ids = $3, degraded_runtime_id = $4, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteRuntimePool :exec
DELETE FROM runtime_pool WHERE id = $1;

-- name: CountAgentsUsingRuntimePool :one
SELECT COUNT(*) FROM agent WHERE runtime_pool_id = $1 AND status <> 'archived';

-- name: SetAgentRuntimePool :one
UPDATE agent SET runtime_pool_id = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: SetTaskFailover :one
UPDATE agent_task_queue SET runtime_id = $2, failover_history = $3 WHERE id = $1 RETURNING *;

-- name: SetTaskFailureReason :exec
UPDATE agent_task_queue SET failure_reason = $2 WHERE id = $1;

-- name: ListWaitingTasksOnOfflineRuntimesWithPool :many
-- Tasks still waiting to be claimed whose runtime has been offline beyond the
-- reconnect grace, for agents that declared a pool: candidates for a move.
SELECT task.* FROM agent_task_queue task
JOIN agent_runtime runtime ON runtime.id = task.runtime_id
JOIN agent ON agent.id = task.agent_id
WHERE task.status IN ('queued', 'deferred')
  AND runtime.status = 'offline'
  AND COALESCE(runtime.last_seen_at, runtime.updated_at) < now() - make_interval(secs => @reconnect_grace_secs::double precision)
  AND agent.runtime_pool_id IS NOT NULL
ORDER BY task.created_at
LIMIT @max_per_tick::int;

-- name: ListIssueTaskFailovers :many
SELECT * FROM agent_task_queue WHERE issue_id = $1 AND failover_history IS NOT NULL ORDER BY created_at DESC LIMIT 50;

-- name: PurgeWorkspaceRuntimePools :exec
DELETE FROM runtime_pool WHERE workspace_id = $1;
