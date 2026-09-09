-- Twenty CRM webhooks (OS plan, chantier 2) become triage items through a
-- source of kind 'twenty' whose ref_id is the workspace id, one per
-- workspace like email intake. The list continues 512/535/610.
ALTER TABLE triage_source DROP CONSTRAINT IF EXISTS triage_source_kind_check;
ALTER TABLE triage_source ADD CONSTRAINT triage_source_kind_check CHECK (kind IN (
    'autopilot_webhook',
    'autopilot_schedule',
    'channel',
    'agent_create',
    'quick_create',
    'meeting',
    'email',
    'twenty'
));
