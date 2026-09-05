DELETE FROM postmortem WHERE trigger = 'taskforce_dissolved';
ALTER TABLE postmortem DROP CONSTRAINT IF EXISTS postmortem_trigger_check;
ALTER TABLE postmortem ADD CONSTRAINT postmortem_trigger_check
    CHECK (trigger IN ('failed', 'costly'));
