CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_scim_token_hash ON scim_token (token_hash) WHERE active;
