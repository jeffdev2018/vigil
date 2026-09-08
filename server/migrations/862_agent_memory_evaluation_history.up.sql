CREATE INDEX CONCURRENTLY agent_memory_evaluation_history ON agent_memory_evaluation (workspace_id, agent_id, memory_id, created_at DESC);
