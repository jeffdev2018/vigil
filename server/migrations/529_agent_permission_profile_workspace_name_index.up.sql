CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_agent_permission_profile_workspace_name ON agent_permission_profile (workspace_id, name);
