-- Snooze: a pending item a human parked until a chosen time. It stays
-- `pending` (nothing is resolved, nothing is lost) but drops out of the
-- default queue listing until snoozed_until passes.
ALTER TABLE triage_item ADD COLUMN IF NOT EXISTS snoozed_until TIMESTAMPTZ;

COMMENT ON COLUMN triage_item.snoozed_until IS
    'When set and still in the future, the item is hidden from the default '
    'pending listing. The triage sweep clears it at due time and re-announces '
    'the item with triage:new.';
