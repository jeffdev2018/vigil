-- The issue's groups, newest first — the only read pattern GET
-- /api/issues/{id}/run-groups has.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_run_group_issue_created ON run_group (issue_id, created_at DESC);
