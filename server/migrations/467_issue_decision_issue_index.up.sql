CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_decision_issue ON issue_decision (issue_id, created_at DESC);
