-- Backing index for the benchmark history (JEF-276), which reads one
-- workspace's benchmark runs newest first. Own single-statement migration so
-- CONCURRENTLY runs outside an implicit transaction (repo convention).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_eval_run_benchmark
    ON eval_run (workspace_id, benchmark, started_at DESC);
