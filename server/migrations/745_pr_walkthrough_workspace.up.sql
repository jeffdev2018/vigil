-- Workspace teardown deletes by workspace_id and would otherwise scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pr_walkthrough_workspace
    ON pr_walkthrough (workspace_id);
