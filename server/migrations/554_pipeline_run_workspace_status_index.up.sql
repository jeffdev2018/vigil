CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pipeline_run_workspace_status ON pipeline_run (workspace_id, status);
