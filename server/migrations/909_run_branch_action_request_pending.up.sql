-- JEF-255: the heartbeat probe and claim. Every daemon heartbeat asks "is
-- there a branch action waiting for this runtime", and the claim takes the
-- oldest one, so neither may become a scan of the request history. Partial on
-- status='pending' keeps the index the size of the work actually outstanding,
-- which is normally zero rows. Mirrors idx_worktree_revert_request_pending
-- (migration 759).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_run_branch_action_request_pending
    ON run_branch_action_request (runtime_id, created_at)
    WHERE status = 'pending';
