-- R01: widen issue_dependency.type with 'duplicate'. Like 'related', a
-- duplicate link is symmetric — it has no direction to normalize, and the
-- handler's duplicate-existence check looks both ways. The CHECK was inline
-- in migration 001 under PostgreSQL's canonical name.
ALTER TABLE issue_dependency DROP CONSTRAINT IF EXISTS issue_dependency_type_check;
ALTER TABLE issue_dependency ADD CONSTRAINT issue_dependency_type_check
    CHECK (type IN ('blocks', 'blocked_by', 'related', 'duplicate'));
