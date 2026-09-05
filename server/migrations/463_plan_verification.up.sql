-- F17: one row per verification run. The run itself is an ordinary
-- agent_task_queue row; task_id is what marks it as a verification run (no
-- kind column), and findings arrive from the agent through
-- POST /api/issues/{id}/plan/verifications/{runId}. Counters are denormalized
-- so the done gate reads one row.
CREATE TABLE IF NOT EXISTS plan_verification (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL,
    issue_id       UUID NOT NULL,
    plan_id        UUID NOT NULL,
    plan_version   INTEGER NOT NULL,
    task_id        UUID NOT NULL,
    -- The run whose completion triggered this verification; one verification
    -- per completed run, and a completed verification run never spawns another.
    source_task_id UUID NOT NULL,
    state          TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'reported', 'failed')),
    -- [{severity, title, detail, files[], plan_step_id}] as reported; severity
    -- is free text on the wire, counters below only know the four known ones.
    findings       JSONB NOT NULL DEFAULT '[]'::jsonb,
    critical_count INTEGER NOT NULL DEFAULT 0,
    major_count    INTEGER NOT NULL DEFAULT 0,
    minor_count    INTEGER NOT NULL DEFAULT 0,
    outdated_count INTEGER NOT NULL DEFAULT 0,
    summary        TEXT,
    reported_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE plan_verification IS
    'Verification run of an issue plan (F17): state, findings and per-severity counters. task_id links the agent_task_queue row that produced it. No FK by house rule.';
