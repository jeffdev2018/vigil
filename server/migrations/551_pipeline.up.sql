-- Pipelines (K37): an ordered chain of typed stages, each routed to an
-- agent or a squad, with an optional human gate before it. A pipeline run
-- moves one issue through the chain; the cursor lives here, not on issue.
CREATE TABLE pipeline (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name         TEXT NOT NULL,
    archived_at  TIMESTAMPTZ,
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE pipeline_stage (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id         UUID NOT NULL,
    workspace_id        UUID NOT NULL,
    position            INTEGER NOT NULL CHECK (position >= 0),
    name                TEXT NOT NULL,
    executor_type       TEXT NOT NULL CHECK (executor_type IN ('agent', 'squad')),
    executor_id         UUID NOT NULL,
    requires_human_gate BOOLEAN NOT NULL DEFAULT false
);
CREATE TABLE pipeline_run (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    issue_id         UUID NOT NULL,
    pipeline_id      UUID NOT NULL,
    current_stage_id UUID,
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'completed', 'cancelled')),
    gate_decision_id UUID,
    last_error       TEXT,
    started_by       UUID,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);
