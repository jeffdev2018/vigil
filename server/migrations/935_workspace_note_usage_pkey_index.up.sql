-- Backing index for workspace_note_usage's primary key, attached in 936 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_note_usage_pkey_uidx
    ON workspace_note_usage (id);
