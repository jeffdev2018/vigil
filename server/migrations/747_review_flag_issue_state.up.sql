-- The issue panel's only read: this issue's flags, filtered by state.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_review_flag_issue_state
    ON review_flag (issue_id, state);
