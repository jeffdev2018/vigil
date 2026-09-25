DELETE FROM workspace_note WHERE source = 'decision';
ALTER TABLE workspace_note DROP CONSTRAINT IF EXISTS workspace_note_source_check;
ALTER TABLE workspace_note ADD CONSTRAINT workspace_note_source_check
    CHECK (source IN ('manual', 'agent', 'curation', 'capture'))
    NOT VALID;
