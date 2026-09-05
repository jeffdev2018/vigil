CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_contest_target
    ON contest (workspace_id, target_type, target_id, created_at DESC);
