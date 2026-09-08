CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_insight_query_log_workspace_recent
    ON insight_query_log (workspace_id, created_at DESC);
