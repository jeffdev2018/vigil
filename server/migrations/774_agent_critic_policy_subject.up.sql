CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_agent_critic_policy_subject ON agent_critic_policy (workspace_id, subject_type, subject_id);
