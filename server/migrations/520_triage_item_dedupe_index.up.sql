-- Transport-level idempotency: at most one pending item per (source,
-- dedupe_key). Partial (pending only) so a redelivery after resolution can
-- start a fresh item, and dedupe_key <> '' so unsigned senders (no key) are
-- not collapsed onto each other.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_triage_item_dedupe
    ON triage_item (workspace_id, source_id, dedupe_key)
    WHERE state = 'pending' AND dedupe_key <> '';
