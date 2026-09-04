ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS resumed_by_task_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS pause_requested_at;
ALTER TABLE agent_task_queue DROP CONSTRAINT IF EXISTS agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled', 'waiting_local_directory', 'deferred'));
