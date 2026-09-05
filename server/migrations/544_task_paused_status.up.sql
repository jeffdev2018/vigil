-- Pause, steer, resume (K19): a run may stop at a safe boundary and wait for
-- a human instruction, then continue in the same runtime session. 'paused'
-- joins the status check; pause_requested_at is the flag the daemon polls;
-- resumed_by_task_id points at the run that continued the session. The
-- steering instruction itself is a task_message of type steering_instruction.
ALTER TABLE agent_task_queue DROP CONSTRAINT IF EXISTS agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled', 'waiting_local_directory', 'deferred', 'paused'));
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS pause_requested_at TIMESTAMPTZ;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS resumed_by_task_id UUID;
