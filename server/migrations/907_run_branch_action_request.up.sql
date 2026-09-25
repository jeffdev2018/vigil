-- JEF-255: one user request to promote or discard the branch a terminal run
-- delivered.
--
-- Same lifecycle as worktree_revert_request (F09, migration 758), and durable
-- for the same reason: the daemon that owns the repository is the only party
-- that can push or delete the branch, so this is a queue — POST enqueues a
-- row, the runtime's next heartbeat claims it, and the daemon posts the
-- outcome back. The row is the audit trail of who asked for a destructive
-- action against the user's own repository, and it survives a server restart
-- mid-flight.
--
-- No FOREIGN KEY, per the repository rule. task_id, runtime_id and
-- workspace_id are resolved and validated in application code, and the rows
-- are cleaned up at workspace teardown.
--
-- status is the whole lifecycle: pending (waiting for the daemon's
-- heartbeat), claimed (the daemon has it), completed, failed. error carries
-- the daemon's named cause for a refusal — no origin remote, a branch checked
-- out elsewhere — and is shown to the user verbatim. pr_url is what the
-- server opened after a successful promote push; it stays empty when no VCS
-- provider covers the remote.
--
-- The CHECKs are added NOT VALID so creating the table never scans it; 908
-- validates them separately.
CREATE TABLE IF NOT EXISTS run_branch_action_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    task_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    branch_name TEXT NOT NULL,
    base_branch TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT run_branch_action_request_action_check
        CHECK (action IN ('promote', 'discard')) NOT VALID,
    CONSTRAINT run_branch_action_request_status_check
        CHECK (status IN ('pending', 'claimed', 'completed', 'failed')) NOT VALID
);

COMMENT ON TABLE run_branch_action_request IS
    'JEF-255: one user request to promote (push + PR) or discard (delete branch and worktree) the branch a terminal run delivered.';
