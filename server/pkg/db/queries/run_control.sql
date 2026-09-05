-- Pause, steer, resume (K19).

-- name: GetControllableTaskForIssue :one
-- The run a human may pause, steer or resume: the latest running or paused one.
SELECT * FROM agent_task_queue
WHERE issue_id = $1 AND (status = 'running' OR (status = 'paused' AND resumed_by_task_id IS NULL))
ORDER BY created_at DESC LIMIT 1;

-- name: RequestTaskPause :one
UPDATE agent_task_queue SET pause_requested_at = COALESCE(pause_requested_at, now())
WHERE id = $1 AND status = 'running' RETURNING *;

-- name: MarkTaskPaused :one
UPDATE agent_task_queue
SET status = 'paused', pause_requested_at = NULL,
    session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir = COALESCE(sqlc.narg('work_dir'), work_dir),
    branch_name = COALESCE(sqlc.narg('branch_name'), branch_name)
WHERE id = $1 AND status = 'running' RETURNING *;

-- name: MarkTaskResumed :one
UPDATE agent_task_queue SET resumed_by_task_id = $2, completed_at = now()
WHERE id = $1 AND status = 'paused' RETURNING *;

-- name: SetTaskResumeContext :exec
UPDATE agent_task_queue SET session_id = $2, work_dir = $3 WHERE id = $1;

-- name: NextTaskMessageSeq :one
SELECT COALESCE(MAX(seq), 0)::int + 1 FROM task_message WHERE task_id = $1;

-- name: ListSteeringInstructions :many
SELECT * FROM task_message WHERE task_id = $1 AND type = 'steering_instruction' ORDER BY seq;
