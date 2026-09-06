-- F09 (JEF-26): a user's request to put a conversation branch back to an
-- earlier turn.
--
-- A durable table rather than the Redis pending-request store the other daemon
-- round trips use (local skill import, CLI auth): those are lookups whose worst
-- outcome is a stale spinner, while this one moves a ref in the user's own
-- repository and then deletes runs and messages. It has to be auditable after
-- the fact and it has to survive a server restart mid-flight.
--
-- No FOREIGN KEY, per the repository rule. runtime_id, target_task_id, issue_id
-- and chat_session_id are resolved and validated in application code, and the
-- rows are cleaned up there too.
--
-- status is the whole lifecycle: pending (waiting for the daemon to pick it up
-- from a heartbeat), claimed (the daemon has it), done, failed. error carries
-- the daemon's named cause for a refusal — a branch that moved, a checkpoint
-- that is gone — and is shown to the user verbatim.
CREATE TABLE IF NOT EXISTS worktree_revert_request (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    target_task_id UUID NOT NULL,
    issue_id UUID,
    chat_session_id UUID,
    requested_by UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    settled_at TIMESTAMPTZ,
    CONSTRAINT worktree_revert_request_status_check
        CHECK (status IN ('pending', 'claimed', 'done', 'failed')),
    CONSTRAINT worktree_revert_request_conversation_check
        CHECK (issue_id IS NOT NULL OR chat_session_id IS NOT NULL)
);

COMMENT ON TABLE worktree_revert_request IS
    'F09: one user request to revert a conversation branch to the turn target_task_id delivered.';
