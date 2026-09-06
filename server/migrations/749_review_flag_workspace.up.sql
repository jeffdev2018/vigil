-- Workspace teardown deletes by workspace_id and would otherwise scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_review_flag_workspace
    ON review_flag (workspace_id);
