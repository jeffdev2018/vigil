CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meeting_workspace_started ON meeting (workspace_id, started_at DESC);
