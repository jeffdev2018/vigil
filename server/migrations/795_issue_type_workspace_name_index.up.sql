-- Display names are unique among ACTIVE types only: archiving frees the name
-- for reuse while the key stays taken.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_type_workspace_name_active
    ON issue_type (workspace_id, lower(name))
    WHERE archived_at IS NULL;
