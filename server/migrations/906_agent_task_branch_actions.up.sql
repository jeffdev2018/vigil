-- JEF-255: promote / discard markers on a run.
--
-- Every terminal worktree run delivers a branch (agent/<agent>/<issue>).
-- Promote pushes that branch to origin and opens a pull request for it;
-- discard deletes branch and worktree. Both are explicit user actions on a
-- terminal run, requested through run_branch_action_request (907) and executed
-- by the daemon that owns the repository.
--
-- promoted_at / discarded_at are the run's terminal facts: set once, by the
-- daemon's result report, and never cleared. promote_pr_url carries the pull
-- request the server opened after the push; it stays empty when the workspace
-- has no VCS provider for the remote — the push alone already satisfies
-- "promote".
--
-- No CHECK constraints and no FOREIGN KEYs, per the repository rule: the
-- guards (terminal status, non-empty branch, not already promoted/discarded)
-- are compare-and-swap updates in application code, so no validate migration
-- is needed here.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS promoted_at TIMESTAMPTZ;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS promote_pr_url TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS discarded_at TIMESTAMPTZ;

COMMENT ON COLUMN agent_task_queue.promoted_at IS
    'JEF-255: when the run''s branch was pushed to origin at the user''s request. NULL until a promote completes.';
COMMENT ON COLUMN agent_task_queue.promote_pr_url IS
    'JEF-255: the pull request opened for the promoted branch. Empty when the workspace has no VCS provider for the remote (the push alone satisfies promote).';
COMMENT ON COLUMN agent_task_queue.discarded_at IS
    'JEF-255: when the run''s branch and worktree were deleted at the user''s request. NULL until a discard completes.';
