-- Working set for the runtime router's statistics read (JEF-237): terminal
-- runs per (runtime, task_class). Single-statement migration so the
-- concurrent build never wraps in a transaction.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_routing_stats
    ON agent_task_queue (runtime_id, task_class)
    WHERE status IN ('completed', 'failed');
