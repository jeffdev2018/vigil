-- delegate_filters and involves_user_id both look issues up by their delegate
-- inside one workspace, which would otherwise scan the whole issue table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_delegate
    ON issue (workspace_id, delegate_type, delegate_id);
