CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_user_calendar_feed_user ON user_calendar_feed (workspace_id, user_id);
