-- Due-snooze lookup for the sweep and the "Snoozed" tab count: only pending
-- items that actually carry a snooze.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_triage_item_snoozed
    ON triage_item (workspace_id, snoozed_until)
    WHERE state = 'pending' AND snoozed_until IS NOT NULL;
