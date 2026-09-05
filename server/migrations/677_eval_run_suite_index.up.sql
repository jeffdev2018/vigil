CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_eval_run_suite ON eval_run (suite_id, started_at DESC);
