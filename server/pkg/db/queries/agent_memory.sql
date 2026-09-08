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
-- Claim-time brief injection: active, non-expired facts, newest first. The
-- caller applies the brief character budget and reverses into chronological order.
SELECT * FROM agent_memory
WHERE agent_id = $1 AND workspace_id = $2
AND status = 'active'
AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at DESC, id DESC
LIMIT 200;

-- name: GetAgentMemory :one
SELECT * FROM agent_memory
WHERE id = $1 AND workspace_id = $2;

-- name: CreateAgentMemory :one
INSERT INTO agent_memory (workspace_id, agent_id, content, source, source_task_id, status, expires_at, source_review)
VALUES ($1, $2, $3, $4, $5, COALESCE(sqlc.narg('status')::text, 'active'), sqlc.narg(expires_at), sqlc.narg(source_review))
RETURNING *;

-- name: UpdateAgentMemoryContent :one
UPDATE agent_memory SET
    content = COALESCE(sqlc.narg('content'), content),
    status = COALESCE(sqlc.narg('status'), status),
    expires_at = CASE WHEN sqlc.arg(update_expiration)::boolean THEN sqlc.narg(expires_at)::timestamptz ELSE expires_at END,
    revision = revision + 1,
    reviewed_by = sqlc.narg('reviewed_by'),
    reviewed_at = now(),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: LockAgentForMemoryUpdate :one
-- All memory writers serialize on the parent, including extraction workers.
SELECT id FROM agent WHERE id = $1 AND workspace_id = $2 FOR UPDATE;

-- name: DeleteAgentMemory :execrows
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteSkill.
WITH cleared_evaluations AS (DELETE FROM agent_memory_evaluation WHERE workspace_id = $2 AND $1 = ANY(memory_ids)),
cleared_versions AS (DELETE FROM agent_memory_version WHERE memory_id = $1 AND workspace_id = $2)
DELETE FROM agent_memory WHERE agent_memory.id = $1 AND agent_memory.workspace_id = $2;

-- name: CountAgentMemories :one
SELECT COUNT(*) FROM agent_memory
WHERE agent_id = $1 AND workspace_id = $2;

-- name: DeleteAgentMemoriesForAgent :exec
-- Application-side cleanup for the one agent-deletion path that has no
-- workspace sweep: agent-builder carrier agents (no FK by repo rule).
WITH cleared_evaluations AS (DELETE FROM agent_memory_evaluation WHERE agent_id = $1),
cleared_versions AS (DELETE FROM agent_memory_version WHERE agent_id = $1)
DELETE FROM agent_memory WHERE agent_memory.agent_id = $1;

-- name: SaveAgentMemoryVersion :exec
INSERT INTO agent_memory_version (memory_id, workspace_id, agent_id, revision, content, status, source, source_task_id, reviewed_by, reviewed_at, created_at, updated_at, expires_at, restored_from_revision, source_review)
SELECT m.id, m.workspace_id, m.agent_id, m.revision, m.content, m.status, m.source, m.source_task_id, m.reviewed_by, m.reviewed_at, m.created_at, m.updated_at, m.expires_at, sqlc.narg(restored_from_revision)::integer, m.source_review
FROM agent_memory m WHERE m.id = sqlc.arg(memory_id) AND m.workspace_id = sqlc.arg(workspace_id)
ON CONFLICT (memory_id, revision) DO NOTHING;

-- name: GetAgentMemoryVersion :one
SELECT * FROM agent_memory_version WHERE memory_id = $1 AND workspace_id = $2 AND revision = $3;

-- name: ListAgentMemoryVersions :many
SELECT * FROM agent_memory_version WHERE memory_id = $1 AND workspace_id = $2 AND revision < $3
ORDER BY revision DESC LIMIT 21;

-- name: GetAgentMemoryForCorrection :one
SELECT * FROM agent_memory
WHERE agent_id = $1 AND workspace_id = $2 AND source_review->>'review_id' = sqlc.arg(review_id)::text;

-- name: GetAgentMemoryCorrectionSource :one
SELECT review.* FROM issue_delivery_review review
JOIN issue ON issue.id = review.issue_id AND issue.workspace_id = review.workspace_id
JOIN agent_task_queue task ON task.id = review.task_id AND task.issue_id = issue.id
WHERE review.id = $1 AND review.workspace_id = $2 AND task.agent_id = $3
AND task.chat_session_id IS NULL;
