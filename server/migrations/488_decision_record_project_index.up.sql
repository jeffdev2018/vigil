CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_decision_record_project ON decision_record (project_id, created_at DESC);
