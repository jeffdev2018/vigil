-- One Linear installation per workspace: the connect flow upserts on this key.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_linear_installation_workspace
    ON linear_installation (workspace_id);
