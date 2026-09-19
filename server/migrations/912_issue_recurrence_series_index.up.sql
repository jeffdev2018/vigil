CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_recurrence_series ON issue (recurrence_id) WHERE recurrence_id IS NOT NULL;
