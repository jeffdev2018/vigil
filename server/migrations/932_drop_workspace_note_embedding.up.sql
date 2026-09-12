-- JEF-412: one vector per note is replaced by one vector per passage
-- (workspace_note_passage.embedding). The old vectors are not migrated: the
-- embedding backfill recomputes them per passage.
DROP TABLE IF EXISTS workspace_note_embedding;
