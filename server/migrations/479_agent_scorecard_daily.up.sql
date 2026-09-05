-- Scorecards (K25): one row per workspace, agent, runtime and UTC day,
-- recomputed by the rollup from the runs that ended that day. Recomputing
-- a day is idempotent; runtime_id uses the zero uuid when a run has none so
-- the unique key stays simple. No foreign keys by house rule; purged in
-- workspace_delete.sql.
CREATE TABLE IF NOT EXISTS agent_scorecard_daily (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL,
    agent_id             UUID NOT NULL,
    runtime_id           UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    day                  DATE NOT NULL,
    runs_total           INTEGER NOT NULL DEFAULT 0,
    runs_failed          INTEGER NOT NULL DEFAULT 0,
    runs_cancelled       INTEGER NOT NULL DEFAULT 0,
    runs_accepted        INTEGER NOT NULL DEFAULT 0,
    runs_reopened        INTEGER NOT NULL DEFAULT 0,
    runs_no_intervention INTEGER NOT NULL DEFAULT 0,
    cost_usd_ticks_total BIGINT NOT NULL DEFAULT 0,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
