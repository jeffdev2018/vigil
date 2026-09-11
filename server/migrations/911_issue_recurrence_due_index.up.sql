CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_recurrence_due ON issue_recurrence (workspace_id, enabled, next_run_at);
