CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_goal
    ON issue (goal_id) WHERE goal_id IS NOT NULL;
