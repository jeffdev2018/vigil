CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_critic_verdict_critic_task ON agent_critic_verdict (critic_task_id) WHERE critic_task_id IS NOT NULL;
