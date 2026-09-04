-- Backing index for triage_source's primary key, attached in 453 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS triage_source_pkey_uidx
    ON triage_source (id);
