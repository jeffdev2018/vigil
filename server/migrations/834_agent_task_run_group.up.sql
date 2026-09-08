-- What an attempt carries beyond an ordinary run (F11 / JEF-6).
--
-- run_group_id is the whole feature's hinge. NULL on every row that exists
-- today and on every run launched outside a group forever after, which is what
-- makes the claim's added conjunct and the pending-slot index's added predicate
-- provably behaviour-preserving: both are guarded on IS NOT NULL.
--
-- model_override lets two attempts of the SAME agent differ by model without
-- cloning the agent. NULL means "use agent.model", the pre-F11 behaviour and
-- the only behaviour outside a group. Validated at creation against the
-- daemon-discovered catalogue; the daemon reads it if set.
--
-- diff_stat / diff_unified hold the consolidated result the daemon computes at
-- Finalize (git diff base..branch). diff_stat is small and always stored;
-- diff_unified is bounded at 256 KiB by the daemon before it is ever sent, and
-- left NULL past that bound so the hottest table in the product does not carry
-- megabytes of patch nobody reads to the end. The UI names the branch instead.
--
-- Nullable, no defaults: a non-volatile default would still be a catalog write
-- on a table this size for no gain, and "absent" is the honest value for every
-- run that is not part of a group.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS run_group_id UUID,
    ADD COLUMN IF NOT EXISTS model_override TEXT,
    ADD COLUMN IF NOT EXISTS diff_stat JSONB,
    ADD COLUMN IF NOT EXISTS diff_unified TEXT;

COMMENT ON COLUMN agent_task_queue.run_group_id IS
    'The racing group this attempt belongs to (F11), NULL for every ordinary run.';
COMMENT ON COLUMN agent_task_queue.model_override IS
    'Model this attempt runs with instead of agent.model (F11), NULL to use the agent''s own.';
COMMENT ON COLUMN agent_task_queue.diff_unified IS
    'Consolidated unified diff of the delivered branch (F11), NULL past the 256 KiB bound — diff_stat still holds the shape.';
