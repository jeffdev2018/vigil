CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_pipeline_run_open_issue ON pipeline_run (issue_id) WHERE status IN ('active', 'paused');
