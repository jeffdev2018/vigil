CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_calendar_event_window ON calendar_event (workspace_id, starts_at);
