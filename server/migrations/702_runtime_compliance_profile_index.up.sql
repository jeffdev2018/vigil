-- One declaration per runtime, and the lookup key for every read (single
-- runtime, or the batch that feeds the enqueue-time compliance filter). Own
-- single-statement migration so CONCURRENTLY runs outside an implicit
-- transaction (repo convention); it also replaces the PRIMARY KEY migration
-- 701 deliberately does not create.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_runtime_compliance_profile_runtime
    ON runtime_compliance_profile (runtime_id);
