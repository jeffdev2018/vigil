-- Backing index for agent_memory's primary key, attached in 464 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_memory_pkey_uidx
    ON agent_memory (id);
