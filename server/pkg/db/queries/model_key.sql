-- K48 · BYOK model keys.

-- name: CreateModelKey :one
INSERT INTO workspace_model_key (id, workspace_id, scope, scope_id, provider, label, key_encrypted, key_hint, priority, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ListModelKeys :many
SELECT * FROM workspace_model_key
WHERE workspace_id = $1
ORDER BY active DESC, scope ASC, provider ASC, priority DESC, created_at DESC;

-- name: GetModelKey :one
SELECT * FROM workspace_model_key
WHERE id = $1 AND workspace_id = $2;

-- name: ListActiveModelKeys :many
-- Candidates for a run: the project's keys first, then the workspace's,
-- highest priority first. Callers pass the issue's project (may be NULL).
SELECT * FROM workspace_model_key
WHERE workspace_id = $1 AND provider = $2 AND active = TRUE
  AND (scope = 'workspace' OR (scope = 'project' AND scope_id = sqlc.narg('project_id')::uuid))
ORDER BY (scope = 'project') DESC, priority DESC, created_at DESC;

-- name: DeactivateModelKey :execrows
UPDATE workspace_model_key
SET active = FALSE, deactivated_reason = $3, deactivated_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND active = TRUE;

-- name: DeactivateModelKeysForScope :exec
-- Rotation: the previous active keys of the same scope and provider step
-- aside; their usage history stays attributed to them.
UPDATE workspace_model_key
SET active = FALSE, deactivated_reason = $4, deactivated_at = now(), updated_at = now()
WHERE workspace_id = $1 AND provider = $2 AND scope = $3 AND scope_id IS NOT DISTINCT FROM sqlc.narg('scope_id')::uuid AND active = TRUE;

-- name: SetTaskModelKey :exec
UPDATE agent_task_queue SET model_key_id = $2 WHERE id = $1;

-- name: ListModelKeyUsage :many
-- Cost attributed per key: token totals and the provider's own price when
-- reported. Priced client-side by model like every other usage view.
SELECT u.model_key_id, u.provider, u.model,
       COUNT(DISTINCT u.task_id)::bigint AS task_count,
       COALESCE(SUM(u.input_tokens), 0)::bigint AS input_tokens,
       COALESCE(SUM(u.output_tokens), 0)::bigint AS output_tokens,
       COALESCE(SUM(u.cache_read_tokens), 0)::bigint AS cache_read_tokens,
       COALESCE(SUM(u.cache_write_tokens), 0)::bigint AS cache_write_tokens,
       COALESCE(SUM(u.cost_usd_ticks), 0)::bigint AS cost_usd_ticks
FROM task_usage u
JOIN workspace_model_key k ON k.id = u.model_key_id
WHERE k.workspace_id = $1
GROUP BY u.model_key_id, u.provider, u.model;

-- name: PurgeWorkspaceModelKeys :exec
DELETE FROM workspace_model_key WHERE workspace_id = $1;
