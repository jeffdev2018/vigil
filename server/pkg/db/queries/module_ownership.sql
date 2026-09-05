-- Module ownership (K33).

-- name: ListModuleOwnership :many
SELECT * FROM module_ownership
WHERE workspace_id = $1
ORDER BY priority DESC, created_at DESC;

-- name: CreateModuleOwnership :one
INSERT INTO module_ownership (workspace_id, path_pattern, label_id, owner_user_id, referent_agent_id, priority)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: DeleteModuleOwnership :execrows
DELETE FROM module_ownership WHERE id = $1 AND workspace_id = $2;
