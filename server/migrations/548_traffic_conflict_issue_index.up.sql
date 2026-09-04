CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_traffic_conflict_issue ON traffic_conflict (issue_id, created_at DESC);
