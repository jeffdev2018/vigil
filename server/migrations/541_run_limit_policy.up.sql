-- Run limits (K03): enforceable caps on a single run — cost, duration,
-- turns, tool calls — declared at workspace, project or agent scope; the
-- most restrictive cap per gate applies. Period budgets (F21,
-- budget_policy) cap the spend of many runs; this caps one run while it
-- runs: warn at warn_bps, stop (failed, reason budget_exceeded) at 100%
-- when enforced, only record when observed. Events dedupe the alerts and
-- tell the run's story.
CREATE TABLE run_limit_policy (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL,
    scope_type           TEXT NOT NULL CHECK (scope_type IN ('workspace', 'project', 'agent')),
    scope_id             UUID,
    max_cost_usd_ticks   BIGINT CHECK (max_cost_usd_ticks IS NULL OR max_cost_usd_ticks > 0),
    max_duration_seconds INTEGER CHECK (max_duration_seconds IS NULL OR max_duration_seconds > 0),
    max_turns            INTEGER CHECK (max_turns IS NULL OR max_turns > 0),
    max_tool_calls       INTEGER CHECK (max_tool_calls IS NULL OR max_tool_calls > 0),
    warn_bps             INTEGER NOT NULL DEFAULT 8000 CHECK (warn_bps BETWEEN 0 AND 10000),
    action               TEXT NOT NULL DEFAULT 'enforce' CHECK (action IN ('observe', 'enforce')),
    created_by           UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((scope_type = 'workspace' AND scope_id IS NULL) OR (scope_type IN ('project', 'agent') AND scope_id IS NOT NULL))
);
CREATE TABLE run_limit_event (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    task_id      UUID NOT NULL,
    policy_id    UUID NOT NULL,
    gate         TEXT NOT NULL CHECK (gate IN ('cost', 'duration', 'turns', 'tool_calls')),
    level        TEXT NOT NULL CHECK (level IN ('warn', 'exceeded', 'stopped')),
    observed     BIGINT NOT NULL,
    limit_value  BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
