CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_share_link_task_active ON task_share_link (task_id) WHERE revoked_at IS NULL;
