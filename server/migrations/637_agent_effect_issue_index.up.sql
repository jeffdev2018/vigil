-- The issue page lists a run's effects newest first; the undo window check
-- reads the same order.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_effect_workspace_issue
    ON agent_effect (workspace_id, issue_id, created_at DESC);
