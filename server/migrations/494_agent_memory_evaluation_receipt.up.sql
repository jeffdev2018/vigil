CREATE UNIQUE INDEX CONCURRENTLY agent_memory_evaluation_receipt ON agent_memory_evaluation (workspace_id, memory_id, report_hash);
