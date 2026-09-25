-- JEF-256: per-project sandbox policy. One row per project; row present means
-- an explicit policy, no row means the project inherits the workspace default.

-- name: GetProjectSandboxPolicy :one
SELECT * FROM project_sandbox_policy WHERE project_id = $1;

-- name: UpsertProjectSandboxPolicy :one
INSERT INTO project_sandbox_policy (project_id, network_mode, allowed_hosts, block_sensitive_files)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id) DO UPDATE SET
    network_mode = EXCLUDED.network_mode,
    allowed_hosts = EXCLUDED.allowed_hosts,
    block_sensitive_files = EXCLUDED.block_sensitive_files,
    updated_at = now()
RETURNING *;

-- name: DeleteProjectSandboxPolicy :execrows
DELETE FROM project_sandbox_policy WHERE project_id = $1;
