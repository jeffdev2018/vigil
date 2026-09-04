-- Collapse key: at most one pending item per (source, normalized_title).
-- UNIQUE so the capture upsert can atomically fold a repeat into the existing
-- pending row (collapse_count + 1) via ON CONFLICT instead of racing an
-- insert-then-update.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_triage_item_pending_title
    ON triage_item (workspace_id, source_id, normalized_title)
    WHERE state = 'pending';
