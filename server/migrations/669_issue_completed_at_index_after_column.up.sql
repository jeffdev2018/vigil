-- Rebuilds the completed_at index skipped by 474 on databases where the
-- column (572) did not exist yet; a no-op where 474 already built it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_completed_at ON issue (workspace_id, completed_at) WHERE completed_at IS NOT NULL;
