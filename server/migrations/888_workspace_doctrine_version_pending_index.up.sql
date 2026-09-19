CREATE UNIQUE INDEX CONCURRENTLY idx_workspace_doctrine_version_pending ON workspace_doctrine_version (workspace_id) WHERE status = 'pending';
