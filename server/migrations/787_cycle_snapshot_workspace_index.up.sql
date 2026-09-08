CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cycle_snapshot_workspace
    ON cycle_snapshot (workspace_id);
