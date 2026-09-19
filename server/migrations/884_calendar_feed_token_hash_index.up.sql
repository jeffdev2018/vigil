CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_calendar_feed_token_hash ON calendar_feed_token (token_hash);
