-- Run confidence scoring (JEF-240).

-- name: SetTaskConfidence :one
UPDATE agent_task_queue SET confidence = $2 WHERE id = $1 RETURNING *;
