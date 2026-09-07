-- NOT partial: an archived type still owns its key, because issues left on it
-- keep resolving through this row.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_type_workspace_key
    ON issue_type (workspace_id, key);
