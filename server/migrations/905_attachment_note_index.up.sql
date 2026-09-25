CREATE INDEX CONCURRENTLY idx_attachment_note ON attachment (note_id) WHERE note_id IS NOT NULL;
