-- Working set for the queue listing: postmortems per workspace ordered by
-- arrival, filtered by state.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_postmortem_workspace_state
    ON postmortem (workspace_id, state, created_at DESC);
