-- Checkpoints (K20).

-- name: CheckpointTask :exec
UPDATE agent_task_queue
SET last_checkpoint_seq = GREATEST(COALESCE(last_checkpoint_seq, 0), $2), checkpointed_at = now()
WHERE id = $1 AND status = 'running';

-- name: GetLatestIssueTaskCheckpoint :one
SELECT id, status, failure_reason, last_checkpoint_seq, checkpoint_attempts, checkpointed_at, retry_of_task_id, created_at
FROM agent_task_queue WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1;
