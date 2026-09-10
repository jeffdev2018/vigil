CREATE UNIQUE INDEX CONCURRENTLY idx_workspace_doctrine_version_revision ON workspace_doctrine_version (workspace_id, revision) WHERE revision IS NOT NULL;
