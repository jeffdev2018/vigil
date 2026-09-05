-- Org chart (K75): a dissolved task force drafts a postmortem from its last run.
ALTER TABLE postmortem DROP CONSTRAINT IF EXISTS postmortem_trigger_check;
ALTER TABLE postmortem ADD CONSTRAINT postmortem_trigger_check
    CHECK (trigger IN ('failed', 'costly', 'taskforce_dissolved'));
