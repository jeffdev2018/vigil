CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_issue_type
    ON issue (workspace_id, issue_type) WHERE issue_type IS NOT NULL;
