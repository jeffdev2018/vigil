-- Halt freeze (JEF-257). Setting the workspace halt (K05) freezes every
-- in-flight run through the K19 pause machinery: the daemon honours the
-- pause request at its next safe boundary and the halt marker remembers that
-- the halt — not a human, not K41 preemption — owns the freeze, so lifting
-- the halt resumes exactly those runs.

-- name: RequestWorkspaceHaltFreeze :many
-- Freeze every running run of the workspace: ask for the pause and stamp the
-- halt marker. pause_requested_at keeps an earlier request's timestamp (a
-- human pause in flight is not overwritten), but the marker only lands on
-- rows no halt froze yet. Returns the newly frozen rows.
UPDATE agent_task_queue
SET pause_requested_at = COALESCE(pause_requested_at, now()),
    halt_frozen_at = now()
WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = sqlc.arg('workspace_id'))
  AND status = 'running'
  AND halt_frozen_at IS NULL
RETURNING id, runtime_id;

-- name: ClearUnackedHaltFreeze :exec
-- Lifting the halt: a frozen run whose daemon never acked the pause (offline,
-- or the run ended on its own) must not pause after the lift, so the pause
-- request and the marker both go away. Rows already paused keep both: they
-- are the resume queue.
UPDATE agent_task_queue
SET pause_requested_at = NULL,
    halt_frozen_at = NULL
WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = sqlc.arg('workspace_id'))
  AND halt_frozen_at IS NOT NULL
  AND status = 'running';

-- name: ListHaltFrozenPausedTasks :many
-- The lift's resume queue: runs the halt froze whose daemon acked the pause,
-- not yet resumed. Everything the resume needs (session, work_dir, branch,
-- agent, issue, runtime) rides on the row.
SELECT * FROM agent_task_queue
WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = sqlc.arg('workspace_id'))
  AND status = 'paused'
  AND halt_frozen_at IS NOT NULL
  AND resumed_by_task_id IS NULL
ORDER BY created_at ASC;

-- name: ClearHaltFrozenMarker :exec
-- The marker's job ends when the resume child exists.
UPDATE agent_task_queue SET halt_frozen_at = NULL WHERE id = $1;

-- name: CountHaltFrozenTasks :one
-- Live count for GET /api/run-halt: runs still held by the halt (frozen and
-- running, or frozen and paused). Terminal rows keep a stale marker only
-- until the lift and are deliberately not counted.
SELECT count(*) FROM agent_task_queue
WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = sqlc.arg('workspace_id'))
  AND halt_frozen_at IS NOT NULL
  AND status IN ('running', 'paused');
