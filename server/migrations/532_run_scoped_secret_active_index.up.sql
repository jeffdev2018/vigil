CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_run_scoped_secret_active ON run_scoped_secret (expires_at) WHERE revoked_at IS NULL;
