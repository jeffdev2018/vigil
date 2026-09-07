-- The Insights tab reads "everything I own, plus everything shared with the
-- workspace". This partial index serves the second half without carrying the
-- private rows, which are already covered by the owner index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_insight_widget_workspace_shared
    ON insight_widget (workspace_id)
    WHERE visibility = 'workspace';
