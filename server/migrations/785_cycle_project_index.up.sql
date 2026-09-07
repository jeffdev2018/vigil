CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cycle_workspace_project
    ON cycle (workspace_id, project_id, start_date);
