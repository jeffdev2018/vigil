-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE agent_memory
    ADD CONSTRAINT agent_memory_pkey PRIMARY KEY USING INDEX agent_memory_pkey_uidx;
