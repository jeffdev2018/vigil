CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_review_of ON agent_task_queue (review_of_task_id) WHERE review_of_task_id IS NOT NULL;
