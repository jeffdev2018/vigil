CREATE UNIQUE INDEX CONCURRENTLY agent_memory_evaluation_execution_request ON agent_memory_evaluation (workspace_id, memory_id, execution_request_id) WHERE execution_request_id IS NOT NULL;
