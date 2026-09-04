-- Backing index for postmortem's primary key, attached in 468 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS postmortem_pkey_uidx
    ON postmortem (id);
