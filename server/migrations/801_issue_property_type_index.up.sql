-- The per-type property cap and the applicability read both start from
-- "which properties are scoped to this type in this workspace".
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_property_type_workspace_key
    ON issue_property_type (workspace_id, type_key);
