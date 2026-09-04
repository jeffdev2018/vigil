CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_scorecard_daily_key ON agent_scorecard_daily (workspace_id, agent_id, runtime_id, day);
