CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_calendar_feed_token_user ON calendar_feed_token (workspace_id, user_id);
