CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_plan_issue_version_key ON issue_plan (issue_id, version);
