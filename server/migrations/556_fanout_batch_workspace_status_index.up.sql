CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_fanout_batch_workspace_status ON fanout_batch (workspace_id, status);
