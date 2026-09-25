CREATE INDEX CONCURRENTLY idx_agent_consult_workspace_created ON agent_consult (workspace_id, created_at DESC);
