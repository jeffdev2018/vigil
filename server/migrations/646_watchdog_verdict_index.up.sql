CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_watchdog_verdict_watchdog
    ON watchdog_verdict (watchdog_id, created_at DESC);
