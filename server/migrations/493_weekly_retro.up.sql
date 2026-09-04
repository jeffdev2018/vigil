-- Standup and retro (K34): one generated retro per workspace and week.
-- The summary is recomposed from runs and scorecards at generation time;
-- generated_at is the regenerate rate limit.
CREATE TABLE weekly_retro (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    week_start   DATE NOT NULL,
    summary      JSONB NOT NULL DEFAULT '{}',
    narrative    TEXT NOT NULL DEFAULT '',
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
