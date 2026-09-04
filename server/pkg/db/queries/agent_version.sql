-- Agent versions (K23).

-- name: CreateAgentVersion :one
-- The number is computed in the statement; the unique index turns a
-- concurrent save into an error the caller retries once.
INSERT INTO agent_version (workspace_id, agent_id, version_number, instructions, model, skill_ids, tool_config, note, created_by_type, created_by_id)
SELECT sqlc.arg(workspace_id), sqlc.arg(agent_id),
       COALESCE(MAX(version_number), 0) + 1,
       sqlc.arg(instructions), sqlc.arg(model), sqlc.arg(skill_ids), sqlc.arg(tool_config), sqlc.arg(note), sqlc.arg(created_by_type), sqlc.narg(created_by_id)
FROM agent_version WHERE agent_id = sqlc.arg(agent_id)
RETURNING *;

-- name: ListAgentVersions :many
SELECT * FROM agent_version WHERE agent_id = $1 ORDER BY version_number DESC LIMIT 200;

-- name: GetLatestAgentVersion :one
SELECT * FROM agent_version WHERE agent_id = $1 ORDER BY version_number DESC LIMIT 1;

-- name: GetAgentVersion :one
SELECT * FROM agent_version WHERE id = $1 AND agent_id = $2;

-- name: GetAgentVersionAt :one
-- The version that was active at a point in time: the newest one created
-- before it.
SELECT * FROM agent_version WHERE agent_id = $1 AND created_at <= $2 ORDER BY version_number DESC LIMIT 1;

-- name: SetAgentVersionSkills :exec
-- Rollback applies a snapshot's skills: the junction is replaced wholesale.
DELETE FROM agent_skill WHERE agent_id = $1;

-- name: AddAgentSkillEnabled :exec
INSERT INTO agent_skill (agent_id, skill_id, enabled) VALUES ($1, $2, true) ON CONFLICT DO NOTHING;

-- name: ApplyAgentVersion :one
UPDATE agent
SET instructions = sqlc.arg(instructions), model = NULLIF(sqlc.arg(model)::text, ''), mcp_config = sqlc.arg(mcp_config), custom_args = sqlc.arg(custom_args), runtime_config = sqlc.arg(runtime_config), disabled_runtime_skills = sqlc.arg(disabled_runtime_skills), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
