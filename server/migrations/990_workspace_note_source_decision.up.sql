-- A note mirrored from a decision record (JEF-415 / B04) carries that
-- provenance. NOT VALID keeps this an instant catalog change; every
-- existing row already satisfies the new list, which only adds a value.
SET LOCAL lock_timeout = '2s';
ALTER TABLE workspace_note DROP CONSTRAINT IF EXISTS workspace_note_source_check;
ALTER TABLE workspace_note ADD CONSTRAINT workspace_note_source_check
    CHECK (source IN ('manual', 'agent', 'curation', 'capture', 'decision'))
    NOT VALID;
