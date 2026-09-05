CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_goal_workspace_parent
    ON goal (workspace_id, parent_goal_id);
