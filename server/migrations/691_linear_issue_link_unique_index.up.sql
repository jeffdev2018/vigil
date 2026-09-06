-- A Linear issue mirrors to exactly one Multica issue. This is what makes a
-- redelivered webhook a no-op instead of a second mirror.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_linear_issue_link_remote
    ON linear_issue_link (linear_team_id, linear_issue_id);
