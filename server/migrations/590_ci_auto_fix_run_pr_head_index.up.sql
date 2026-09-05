CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_ci_auto_fix_run_pr_head ON ci_auto_fix_run (pull_request_id, head_sha);
