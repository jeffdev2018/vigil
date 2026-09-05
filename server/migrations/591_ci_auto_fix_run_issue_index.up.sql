CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ci_auto_fix_run_issue ON ci_auto_fix_run (issue_id, created_at DESC);
