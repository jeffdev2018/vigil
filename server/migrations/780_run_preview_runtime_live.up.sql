CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_run_preview_runtime_live ON run_preview (runtime_id) WHERE status IN ('starting', 'ready');
