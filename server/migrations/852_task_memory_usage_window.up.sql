CREATE INDEX CONCURRENTLY idx_agent_task_memory_usage_window ON agent_task_queue (agent_id, started_at) WHERE chat_session_id IS NULL AND started_at IS NOT NULL;
