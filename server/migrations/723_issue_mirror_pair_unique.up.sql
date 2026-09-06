-- A source issue mirrors into a given mirror issue exactly once. This is the
-- durable half of the idempotency guarantee: re-attaching the trigger label
-- must not produce a second mirror, and the service's existence check alone
-- would lose a concurrent double attach.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_issue_mirror_pair
    ON issue_mirror (source_issue_id, mirror_issue_id);
