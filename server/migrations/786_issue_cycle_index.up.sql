CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_cycle
    ON issue (workspace_id, cycle_id) WHERE cycle_id IS NOT NULL;
