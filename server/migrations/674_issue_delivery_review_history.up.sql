CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_delivery_review_history ON issue_delivery_review (workspace_id, issue_id, created_at DESC, id DESC);
