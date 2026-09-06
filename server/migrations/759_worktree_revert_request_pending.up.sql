-- F09: the heartbeat probe. Every daemon heartbeat asks "is there a revert
-- waiting for this runtime", so it must never become a scan of the request
-- history. Partial on status='pending' keeps the index the size of the work
-- actually outstanding, which is normally zero rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_worktree_revert_request_pending
    ON worktree_revert_request (runtime_id, created_at)
    WHERE status = 'pending';
