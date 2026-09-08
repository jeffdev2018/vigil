CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_critic_verdict_issue ON agent_critic_verdict (issue_id, created_at DESC);
