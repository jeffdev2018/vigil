CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_plan_verification_issue ON plan_verification (issue_id, created_at DESC);
