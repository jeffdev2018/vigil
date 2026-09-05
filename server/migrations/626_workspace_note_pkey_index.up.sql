-- Backing index for workspace_note's primary key, attached in 627 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY
-- runs outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_note_pkey_uidx
    ON workspace_note (id);
