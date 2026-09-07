-- One edge per (from, to, type). The handler still checks for an existing
-- dependency to answer 409 with a readable message, but that check races: this
-- index is what actually makes the duplicate impossible. (JEF-145 debt)
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_issue_dependency_edge
    ON issue_dependency (issue_id, depends_on_issue_id, type);
