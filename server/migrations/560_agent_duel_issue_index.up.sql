CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_duel_issue_created ON agent_duel (issue_id, created_at DESC);
