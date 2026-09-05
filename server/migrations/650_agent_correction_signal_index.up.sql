CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_correction_signal_agent
    ON agent_correction_signal (workspace_id, agent_id, detected_at DESC);
