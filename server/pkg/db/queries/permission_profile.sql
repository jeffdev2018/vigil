-- Permission profiles (K06).

-- name: CreatePermissionProfile :one
INSERT INTO agent_permission_profile (id, workspace_id, name, description, read_only, denied_paths, allowed_commands, hidden_secrets, builtin)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListPermissionProfiles :many
SELECT * FROM agent_permission_profile WHERE workspace_id = $1 ORDER BY builtin DESC, created_at, id;

-- name: GetPermissionProfile :one
SELECT * FROM agent_permission_profile WHERE id = $1;

-- name: UpdatePermissionProfileRules :one
UPDATE agent_permission_profile
SET description = $2, read_only = $3, denied_paths = $4, allowed_commands = $5, hidden_secrets = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeletePermissionProfile :execrows
DELETE FROM agent_permission_profile WHERE id = $1 AND builtin = false;

-- name: CountAgentsUsingPermissionProfile :one
SELECT COUNT(*) FROM agent WHERE permission_profile_id = $1;

-- name: SetAgentPermissionProfile :one
UPDATE agent SET permission_profile_id = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: SetAgentTaskPermissionProfile :one
UPDATE agent_task_queue SET permission_profile_id = $2 WHERE id = $1 RETURNING *;

-- name: PurgeWorkspacePermissionProfiles :exec
DELETE FROM agent_permission_profile WHERE workspace_id = $1;
