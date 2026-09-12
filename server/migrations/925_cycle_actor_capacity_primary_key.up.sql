-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE cycle_actor_capacity
    ADD CONSTRAINT cycle_actor_capacity_pkey PRIMARY KEY USING INDEX cycle_actor_capacity_pkey_uidx;
