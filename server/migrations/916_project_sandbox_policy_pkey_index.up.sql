-- Backing index for project_sandbox_policy's primary key, attached in 917 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_sandbox_policy_pkey_uidx
    ON project_sandbox_policy (project_id);
