-- F09 (JEF-26): what a worktree run delivered, recorded per turn.
--
-- checkpoint_sha is the git commit refs/multica/turn/<taskKey> points at in the
-- user's own repository — the turn RECORD, not the branch tip. The record's
-- tree is the user's directory as the branch then carried it and its second
-- parent is the commit the run delivered, so this one value is everything a
-- revert needs to put the branch back where that turn left it.
--
-- Only worktree-mode runs on a conversation get one. In-place runs, read-only
-- runs and task-scoped branches record nothing, and NULL here is what "this run
-- cannot be reverted to" means end to end — the API omits the field and the UI
-- leaves the action out rather than disabling it.
--
-- turn_seq orders the turns of one conversation. Assigned by the server when
-- the terminal report lands, because the daemon cannot see the conversation's
-- other runs. It is what "delete every turn after N" is expressed in.
--
-- Both columns are nullable with no default, so adding them does not rewrite
-- agent_task_queue, the largest table in the database.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS checkpoint_sha TEXT;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS turn_seq INT;

COMMENT ON COLUMN agent_task_queue.checkpoint_sha IS
    'F09: git commit of the turn record this worktree run delivered (refs/multica/turn/<taskKey> in the user repo). NULL means the run is not revertible.';
COMMENT ON COLUMN agent_task_queue.turn_seq IS
    'F09: 1-based position of this run among the checkpointed turns of its conversation. Assigned server-side on the terminal report.';
