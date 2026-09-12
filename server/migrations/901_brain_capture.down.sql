ALTER TABLE attachment DROP COLUMN IF EXISTS note_id;
ALTER TABLE attachment DROP COLUMN IF EXISTS capture_id;
ALTER TABLE workspace_note DROP CONSTRAINT IF EXISTS workspace_note_source_check;
ALTER TABLE workspace_note ADD CONSTRAINT workspace_note_source_check CHECK (source IN ('manual', 'agent', 'curation'));
DROP TABLE IF EXISTS workspace_note_embedding;
DROP TABLE IF EXISTS brain_capture;
