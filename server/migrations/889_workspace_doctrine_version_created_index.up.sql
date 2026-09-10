CREATE INDEX CONCURRENTLY idx_workspace_doctrine_version_ws_created ON workspace_doctrine_version (workspace_id, created_at DESC, id DESC);
