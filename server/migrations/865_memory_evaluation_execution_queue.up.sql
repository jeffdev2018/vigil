CREATE INDEX CONCURRENTLY agent_memory_evaluation_execution_queue ON agent_memory_evaluation (execution_runtime_id, execution_status, created_at) WHERE execution_status IN ('queued','running');
