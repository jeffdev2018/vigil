CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_version_agent_number ON agent_version (agent_id, version_number DESC);
