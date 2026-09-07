CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_insight_widget_workspace_owner
    ON insight_widget (workspace_id, owner_id);
