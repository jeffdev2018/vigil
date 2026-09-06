-- F09: one in-flight revert per target run, enforced by the database.
--
-- The endpoint also refuses a second request for a conversation with 409, but
-- that check and the insert cannot be one atomic step across nodes, and two
-- reverts racing on the same branch is precisely the case that loses work: the
-- second one reads a tip the first has already moved. The unique index is what
-- actually makes the check safe — the losing INSERT fails and the handler turns
-- that into the same 409.
--
-- Partial on pending only: a run may be reverted again after an earlier attempt
-- settled, and the history has to stay insertable.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_worktree_revert_request_target_pending
    ON worktree_revert_request (target_task_id)
    WHERE status = 'pending';
