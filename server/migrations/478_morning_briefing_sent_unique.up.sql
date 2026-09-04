CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS morning_briefing_sent_workspace_date ON morning_briefing_sent (workspace_id, sent_for_date);
