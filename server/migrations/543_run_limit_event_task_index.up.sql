CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_run_limit_event_task ON run_limit_event (task_id, created_at);
