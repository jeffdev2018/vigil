CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trust_mode_change_agent ON trust_mode_change (agent_id, created_at DESC);
