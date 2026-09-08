-- Autopilot execution memory (F24 / JEF-15). One document per autopilot,
-- rewritten by its own runs and re-injected into every following brief.

-- name: GetAutopilotMemory :one
SELECT * FROM autopilot_memory
WHERE autopilot_id = $1 AND workspace_id = $2;

-- name: UpsertAutopilotMemory :one
-- Optimistic concurrency: the caller passes the revision it read, and the
-- ON CONFLICT guard refuses the write when another run has moved on since.
-- A first write races the same way — a concurrent INSERT wins and leaves
-- revision = 1, so the loser's expected_revision = 0 no longer matches and it
-- gets the same 409 as any other stale write instead of clobbering.
INSERT INTO autopilot_memory (autopilot_id, workspace_id, content, revision, updated_by_task_id, updated_at)
VALUES ($1, $2, $3, 1, sqlc.narg('updated_by_task_id'), now())
ON CONFLICT (autopilot_id) DO UPDATE SET
    content = EXCLUDED.content,
    revision = autopilot_memory.revision + 1,
    updated_by_task_id = EXCLUDED.updated_by_task_id,
    updated_at = now()
WHERE autopilot_memory.revision = sqlc.arg('expected_revision')::int
RETURNING *;

-- name: DeleteAutopilotMemory :exec
DELETE FROM autopilot_memory WHERE autopilot_id = $1;

-- name: GetAutopilotMemoryForRun :one
-- The brief path: resolve a task's autopilot from its run in one round trip.
-- workspace_id is a SQL-layer tenant guard, matching every other brief read.
SELECT m.autopilot_id, m.content, m.revision, m.updated_at
FROM autopilot_run r
JOIN autopilot_memory m ON m.autopilot_id = r.autopilot_id
WHERE r.id = $1 AND m.workspace_id = $2;

-- name: DeleteWorkspaceAutopilotMemories :exec
DELETE FROM autopilot_memory WHERE autopilot_memory.workspace_id = $1;
