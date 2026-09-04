-- Agent memory (JEF-236): durable per-agent facts injected into run briefs.

-- name: ListAgentMemories :many
-- Full chronological listing for the REST endpoint. Every read carries the
-- workspace_id tenant guard.
SELECT * FROM agent_memory
WHERE agent_id = $1 AND workspace_id = $2
ORDER BY created_at ASC, id ASC;

-- name: ListRecentAgentMemories :many
-- Claim-time brief injection: the 50 most recent facts, newest first. The
-- caller reverses into chronological order for the prompt.
SELECT * FROM agent_memory
WHERE agent_id = $1 AND workspace_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 50;

-- name: GetAgentMemory :one
SELECT * FROM agent_memory
WHERE id = $1 AND workspace_id = $2;

-- name: CreateAgentMemory :one
INSERT INTO agent_memory (workspace_id, agent_id, content, source, source_task_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateAgentMemoryContent :one
UPDATE agent_memory SET
    content = COALESCE(sqlc.narg('content'), content),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteAgentMemory :execrows
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteSkill.
DELETE FROM agent_memory WHERE id = $1 AND workspace_id = $2;

-- name: CountAgentMemories :one
SELECT COUNT(*) FROM agent_memory
WHERE agent_id = $1 AND workspace_id = $2;

-- name: DeleteOldestRunMemories :exec
-- Eviction after an extraction insert: delete the oldest source='run' rows
-- until the agent is back under the total cap (sqlc.arg(keep_limit)). Manual
-- facts are never evicted, so when manual rows alone fill the cap the run set
-- drains to nothing rather than eating into them.
DELETE FROM agent_memory
WHERE agent_memory.agent_id = sqlc.arg(agent_id) AND agent_memory.source = 'run' AND agent_memory.id IN (
    SELECT agent_memory.id FROM agent_memory
    WHERE agent_memory.agent_id = sqlc.arg(agent_id) AND agent_memory.source = 'run'
    ORDER BY agent_memory.created_at ASC, agent_memory.id ASC
    LIMIT GREATEST(
        (SELECT COUNT(*) FROM agent_memory WHERE agent_memory.agent_id = sqlc.arg(agent_id)) - sqlc.arg(keep_limit)::int,
        0
    )
);

-- name: DeleteAgentMemoriesForAgent :exec
-- Application-side cleanup for the one agent-deletion path that has no
-- workspace sweep: agent-builder carrier agents (no FK by repo rule).
DELETE FROM agent_memory WHERE agent_id = $1;
