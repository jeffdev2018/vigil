-- Recurring issues (OS plan, table stakes): a rule attached to an issue that
-- spawns the next occurrence on a schedule or when the current one closes.
-- Every occurrence (and the source) carries recurrence_id so the series is
-- one lookup. No foreign keys by house rule: the tick deletes a rule whose
-- issue is gone.
CREATE TABLE issue_recurrence (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    cron_expression TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT 'UTC',
    mode TEXT NOT NULL DEFAULT 'schedule' CHECK (mode IN ('schedule', 'on_close')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    next_run_at TIMESTAMPTZ,
    last_occurrence_id UUID,
    occurrence_count INTEGER NOT NULL DEFAULT 0,
    created_by_type TEXT NOT NULL DEFAULT 'member',
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE issue ADD COLUMN IF NOT EXISTS recurrence_id UUID;
