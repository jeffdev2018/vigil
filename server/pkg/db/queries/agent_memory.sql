-- Agent memory (JEF-236): durable per-agent facts injected into run briefs.

-- name: ListAgentMemories :many
-- Full chronological listing for the REST endpoint. Every read carries the
-- workspace_id tenant guard. source_issue_id resolves the run-sourced fact to
-- the issue its task worked on, so the UI can link back to it; NULL for manual
-- facts and for tasks that carried no issue.
SELECT agent_memory.*, t.issue_id AS source_issue_id
FROM agent_memory
LEFT JOIN agent_task_queue t ON t.id = agent_memory.source_task_id
WHERE agent_memory.agent_id = $1 AND agent_memory.workspace_id = $2
ORDER BY agent_memory.created_at ASC, agent_memory.id ASC;

-- name: ListRecentAgentMemories :many
-- Claim-time brief injection: the agent's facts, newest first, bounded by the
-- same per-agent cap the write path enforces. The caller applies the brief
-- character budget and reverses into chronological order for the prompt.
SELECT * FROM agent_memory
WHERE agent_id = $1 AND workspace_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 200;

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
