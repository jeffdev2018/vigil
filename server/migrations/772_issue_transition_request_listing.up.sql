CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_transition_request_listing ON issue_transition_request (workspace_id, state, created_at DESC);
