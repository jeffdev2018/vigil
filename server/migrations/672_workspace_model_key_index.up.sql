CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_model_key_lookup ON workspace_model_key (workspace_id, provider, active, scope);
