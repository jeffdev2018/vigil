CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_decision_search_chunk_workspace ON decision_search_chunk (workspace_id, created_at DESC);
