-- Preemption (K41).

-- name: ListRunningTasksForAgentByPriority :many
-- Lowest priority and oldest first: the first row is the preemption candidate.
SELECT * FROM agent_task_queue
WHERE agent_id = $1 AND status = 'running' AND pause_requested_at IS NULL
ORDER BY priority ASC, created_at ASC;

-- name: MarkTaskPreempted :one
UPDATE agent_task_queue
SET preempted_at = now(), preempted_by_task_id = $2, pause_requested_at = COALESCE(pause_requested_at, now())
WHERE id = $1 AND status = 'running' RETURNING *;

-- name: ListPreemptedPausedTasks :many
-- The resume queue: priority first, then age.
SELECT * FROM agent_task_queue
WHERE status = 'paused' AND preempted_by_task_id IS NOT NULL AND resumed_by_task_id IS NULL
ORDER BY priority DESC, created_at ASC
LIMIT $1;

-- name: ListIssuePreemptions :many
SELECT * FROM agent_task_queue WHERE issue_id = $1 AND preempted_at IS NOT NULL ORDER BY preempted_at DESC LIMIT 20;

-- name: CountQueuedUrgentTasksForAgent :one
SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND status IN ('queued', 'dispatched') AND priority >= 4;

-- name: CountCapacityBearingTasks :one
-- Running plus not-yet-started work: what a freed slot has to compete with.
SELECT count(*) FROM agent_task_queue
WHERE agent_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');
