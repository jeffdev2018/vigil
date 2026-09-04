-- Meetings feed the triage queue: each extracted action item is captured
-- against a triage_source of kind 'meeting' whose ref_id is the meeting row.
-- The inline CHECK from 512 was auto-named by PostgreSQL.
ALTER TABLE triage_source DROP CONSTRAINT IF EXISTS triage_source_kind_check;
ALTER TABLE triage_source ADD CONSTRAINT triage_source_kind_check CHECK (kind IN (
    'autopilot_webhook',
    'autopilot_schedule',
    'channel',
    'agent_create',
    'quick_create',
    'meeting'
));
