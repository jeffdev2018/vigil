-- Per-agent lookups: the claim path, the REST list, and the eviction pass all
-- read an agent's memories filtered by agent_id (plus workspace_id tenant
-- guard), so agent_id is the leading column.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_memory_agent
    ON agent_memory (agent_id);
