-- Backing index for project_review_config's primary key, attached in 678 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_review_config_pkey_uidx
    ON project_review_config (project_id);
