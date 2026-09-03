CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_active_last_activity ON agent_task_queue (last_activity_at) WHERE status IN ('dispatched', 'running', 'waiting_local_directory');
