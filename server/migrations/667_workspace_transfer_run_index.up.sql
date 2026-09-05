CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_transfer_run_workspace
    ON workspace_transfer_run (workspace_id, created_at DESC);
