-- Costly-run postmortems (k68): a workspace may ask for a postmortem when a
-- run that SUCCEEDED cost more than a threshold, not only when one failed.
-- NULL (the default, and the only value for every existing workspace) disables
-- the trigger, so the behaviour of a workspace that never sets it is unchanged.
--
-- Ticks, not dollars: the same 1e-10 USD unit task_usage.cost_usd_ticks and
-- postmortem.cost_usd_ticks already use, so the comparison is integer.
ALTER TABLE workspace
    ADD COLUMN IF NOT EXISTS postmortem_cost_threshold_usd_ticks BIGINT;

COMMENT ON COLUMN workspace.postmortem_cost_threshold_usd_ticks IS
    'Draft a postmortem when a completed run costs more than this many cost_usd_ticks (1e-10 USD). NULL disables the costly trigger.';
