-- Blast radius (K07).

-- name: CreateBlastRadiusRule :one
INSERT INTO project_blast_radius_rule (id, workspace_id, project_id, path_pattern, autonomy_level, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListBlastRadiusRules :many
SELECT * FROM project_blast_radius_rule WHERE workspace_id = $1 AND project_id = $2 ORDER BY created_at ASC, id ASC;

-- name: DeleteBlastRadiusRule :execrows
DELETE FROM project_blast_radius_rule WHERE id = $1 AND workspace_id = $2 AND project_id = $3;

-- name: PurgeWorkspaceBlastRadiusRules :exec
DELETE FROM project_blast_radius_rule WHERE workspace_id = $1;
