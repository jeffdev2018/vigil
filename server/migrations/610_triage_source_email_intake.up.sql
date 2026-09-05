-- Email intake: a workspace can hand out one inbound endpoint whose bearer
-- token IS the credential (no workspace header, no session). The token is
-- stored as its sha256 hex digest, never in clear, like every other
-- server-minted secret (run_scoped_secret.token_hash).
--
-- kind 'email' joins the list from 512/535. Its ref_id is the workspace id:
-- email intake is one source per workspace, not per object.
ALTER TABLE triage_source ADD COLUMN IF NOT EXISTS token_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE triage_source DROP CONSTRAINT IF EXISTS triage_source_kind_check;
ALTER TABLE triage_source ADD CONSTRAINT triage_source_kind_check CHECK (kind IN (
    'autopilot_webhook',
    'autopilot_schedule',
    'channel',
    'agent_create',
    'quick_create',
    'meeting',
    'email'
));
