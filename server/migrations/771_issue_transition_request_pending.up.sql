CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_issue_transition_request_pending ON issue_transition_request (workspace_id, issue_id) WHERE state = 'pending';
