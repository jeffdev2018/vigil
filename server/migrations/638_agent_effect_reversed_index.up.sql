-- Circuit breaker: how many of this agent's runs were undone recently.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_effect_agent_reversed
    ON agent_effect (workspace_id, agent_id, reversed_at)
    WHERE reversed_at IS NOT NULL;
