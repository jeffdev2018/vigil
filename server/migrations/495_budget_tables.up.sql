CREATE TABLE budget_policy (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'project', 'agent')),
    scope_id UUID,
    limit_usd_ticks BIGINT NOT NULL CHECK (limit_usd_ticks > 0),
    period TEXT NOT NULL CHECK (period IN ('daily', 'weekly', 'monthly')),
    warn_bps INTEGER NOT NULL DEFAULT 8000 CHECK (warn_bps BETWEEN 0 AND 10000),
    action TEXT NOT NULL DEFAULT 'enforce' CHECK (action IN ('observe', 'enforce')),
    created_by UUID NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (scope_type = 'workspace' AND scope_id IS NULL)
        OR (scope_type IN ('project', 'agent') AND scope_id IS NOT NULL)
    )
);

CREATE TABLE budget_period (
    policy_id UUID NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    spent_usd_ticks BIGINT NOT NULL DEFAULT 0 CHECK (spent_usd_ticks >= 0),
    reserved_usd_ticks BIGINT NOT NULL DEFAULT 0 CHECK (reserved_usd_ticks >= 0),
    warn_notified_at TIMESTAMPTZ,
    block_notified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_start < period_end)
);

CREATE TABLE budget_reservation (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    task_id UUID NOT NULL,
    estimate_usd_ticks BIGINT NOT NULL CHECK (estimate_usd_ticks >= 0),
    actual_usd_ticks BIGINT,
    state TEXT NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved', 'consumed', 'released')),
    idempotency_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_at TIMESTAMPTZ,
    CHECK (period_start < period_end),
    CHECK (actual_usd_ticks IS NULL OR actual_usd_ticks >= 0)
);

CREATE TABLE budget_override (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    policy_id UUID NOT NULL,
    granted_by UUID NOT NULL,
    reason TEXT NOT NULL CHECK (length(btrim(reason)) > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);
