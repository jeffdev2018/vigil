-- Run-scoped secrets (K09).

-- name: CreateRunScopedSecret :one
INSERT INTO run_scoped_secret (id, workspace_id, task_id, agent_id, key, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetRunScopedSecretByHash :one
SELECT * FROM run_scoped_secret WHERE task_id = $1 AND token_hash = $2;

-- name: ListRunScopedSecretsByTask :many
SELECT * FROM run_scoped_secret WHERE task_id = $1 ORDER BY key;

-- name: ListRunScopedSecretsByIssue :many
SELECT s.* FROM run_scoped_secret s
JOIN agent_task_queue t ON t.id = s.task_id
WHERE t.issue_id = $1
ORDER BY s.created_at DESC, s.key
LIMIT 200;

-- name: RevokeRunScopedSecretsByTask :many
UPDATE run_scoped_secret SET revoked_at = now(), revoke_reason = $2 WHERE task_id = $1 AND revoked_at IS NULL RETURNING *;

-- name: UpdateAgentScopedEnvKeys :one
UPDATE agent SET scoped_env_keys = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: PurgeWorkspaceRunScopedSecrets :exec
DELETE FROM run_scoped_secret WHERE workspace_id = $1;
