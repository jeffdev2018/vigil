DELETE FROM triage_item WHERE source_id IN (SELECT id FROM triage_source WHERE kind = 'email');
DELETE FROM triage_source WHERE kind = 'email';
ALTER TABLE triage_source DROP CONSTRAINT IF EXISTS triage_source_kind_check;
ALTER TABLE triage_source ADD CONSTRAINT triage_source_kind_check CHECK (kind IN (
    'autopilot_webhook',
    'autopilot_schedule',
    'channel',
    'agent_create',
    'quick_create',
    'meeting'
));
ALTER TABLE triage_source DROP COLUMN IF EXISTS token_hash;
