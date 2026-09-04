CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_agent_domain_competency ON agent_domain_competency (workspace_id, agent_id, domain_key);
