-- Brain note kinds (JEF-415 / B04): the shape of durable knowledge a note
-- holds, so injection and search can weigh a decision differently from a
-- glossary entry. A NOT NULL column with a constant default is a fast
-- catalog-only change on PostgreSQL 11+; the CHECK arrives in the next
-- migration so this one stays instant.
ALTER TABLE workspace_note ADD COLUMN kind TEXT NOT NULL DEFAULT 'fact';

COMMENT ON COLUMN workspace_note.kind IS
    'What shape of knowledge this note holds: fact, decision, procedure, glossary or episode.';
