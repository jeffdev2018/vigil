CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_module_ownership_workspace ON module_ownership (workspace_id, priority DESC, created_at DESC);
