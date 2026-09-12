-- Backing index for cycle_actor_capacity's primary key, attached in 922 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS cycle_actor_capacity_pkey_uidx
    ON cycle_actor_capacity (cycle_id, actor_type, actor_id);
