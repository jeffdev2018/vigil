-- name: ListWorkspaceMcpServers :many
-- The workspace's MCP library. Returns `config` because the callers that need
-- it (claim assembly, the write path's read-modify-write) run server-side; the
-- HTTP layer projects it down to the non-secret inventory.
SELECT * FROM workspace_mcp_server
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: LockWorkspaceMcpServerForShare :one
-- Taken by the assignment writer before it inserts. A shared row lock
-- conflicts with the exclusive lock DeleteWorkspaceMcpServer takes, so an
-- assignment can never land in the window between that delete sweeping the
-- bindings and committing — the junction has no FK to catch the orphan.
SELECT id FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2
FOR SHARE;

-- name: LockWorkspaceMcpServerForUpdate :one
-- The delete half of the same protocol: take the row exclusively first, so a
-- concurrent assignment either lands before this transaction (and is swept) or
-- waits and then finds the server gone.
SELECT id FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: GetWorkspaceMcpServer :one
SELECT * FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2;

-- name: CreateWorkspaceMcpServer :one
INSERT INTO workspace_mcp_server (workspace_id, name, config, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateWorkspaceMcpServer :one
-- Full replace of one entry. The name can change here (unlike the previous
-- document model) because bindings key off the id, so a rename cannot orphan
-- the agents that use it.
UPDATE workspace_mcp_server SET
    name = COALESCE(sqlc.narg('name'), name),
    config = COALESCE(sqlc.narg('config'), config),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteWorkspaceMcpServer :execrows
DELETE FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteAgentMcpServersByServer :exec
-- Bindings carry no FK, so removing a library entry must sweep them explicitly
-- in the same transaction as the delete.
DELETE FROM agent_mcp_server WHERE server_id = $1;

-- name: ListAgentMcpServers :many
-- Every workspace server bound to this agent, enabled or not, for the agent
-- settings list. `enabled` comes from the binding.
SELECT s.id, s.workspace_id, s.name, s.config, s.tools, s.created_at, s.updated_at, ams.enabled, ams.tool_policy, ams.tool_usage
FROM workspace_mcp_server s
JOIN agent_mcp_server ams ON ams.server_id = s.id
WHERE ams.agent_id = $1
ORDER BY s.name ASC;

-- name: ListEnabledAgentMcpServers :many
-- Claim path: only bound AND enabled servers reach the runtime. An unbound
-- workspace server is invisible to this agent — creating one gives it to
-- nobody until someone adds it here.
SELECT s.id, s.name, s.config, s.tools, ams.tool_policy
FROM workspace_mcp_server s
JOIN agent_mcp_server ams ON ams.server_id = s.id
WHERE ams.agent_id = $1 AND ams.enabled = TRUE
ORDER BY s.name ASC;

-- name: AddAgentMcpServer :exec
INSERT INTO agent_mcp_server (agent_id, server_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: SetAgentMcpServerEnabled :execrows
UPDATE agent_mcp_server
SET enabled = $3
WHERE agent_id = $1 AND server_id = $2;

-- name: RemoveAgentMcpServer :execrows
DELETE FROM agent_mcp_server
WHERE agent_id = $1 AND server_id = $2;

-- K77 · governed gateway: tool catalogue per server, tool policy per binding.

-- name: GetWorkspaceMcpServerByName :one
SELECT * FROM workspace_mcp_server
WHERE workspace_id = $1 AND name = $2;

-- name: SetWorkspaceMcpServerTools :exec
UPDATE workspace_mcp_server
SET tools = $3, tools_discovered_at = $4, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: ListAgentMcpBindings :many
-- Every binding of this agent with the server's catalogue and the binding's
-- policy: the unit the Rule of Two is checked over.
SELECT s.id AS server_id, s.name, s.tools, ams.enabled, ams.tool_policy, ams.tool_usage
FROM workspace_mcp_server s
JOIN agent_mcp_server ams ON ams.server_id = s.id
WHERE ams.agent_id = $1
ORDER BY s.name ASC;

-- name: SetAgentMcpServerPolicy :execrows
UPDATE agent_mcp_server
SET tool_policy = $3
WHERE agent_id = $1 AND server_id = $2;

-- name: TouchAgentMcpToolUsage :exec
UPDATE agent_mcp_server
SET tool_usage = tool_usage || jsonb_build_object(sqlc.arg(tool)::text, to_jsonb(sqlc.arg(used_at)::timestamptz))
WHERE agent_id = $1 AND server_id = $2;

-- name: ListAgentMcpBindingsForReview :many
-- Monthly review: every enabled binding with a catalogue, with the agent's
-- owner to propose the removal of tools nobody used.
SELECT ams.agent_id, ams.server_id, ams.tool_policy, ams.tool_usage, ams.created_at,
       s.name AS server_name, s.tools, s.workspace_id, a.name AS agent_name, a.owner_id, a.trust_mode
FROM agent_mcp_server ams
JOIN workspace_mcp_server s ON s.id = ams.server_id
JOIN agent a ON a.id = ams.agent_id
WHERE ams.enabled = TRUE AND jsonb_array_length(s.tools) > 0 AND a.archived_at IS NULL;
