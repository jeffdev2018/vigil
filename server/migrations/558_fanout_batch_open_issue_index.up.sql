CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_fanout_batch_open_issue ON fanout_batch (parent_issue_id) WHERE status = 'pending';
