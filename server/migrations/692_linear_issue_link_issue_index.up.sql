-- The outbound half looks a link up by Multica issue on every comment and
-- status change, so it cannot be a scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_linear_issue_link_issue
    ON linear_issue_link (issue_id);
