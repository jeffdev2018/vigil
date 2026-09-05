CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_goal_goal
    ON project_goal (workspace_id, goal_id);
