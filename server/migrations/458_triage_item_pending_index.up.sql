-- Working set for the queue listing and the oldest-pending stat: pending
-- items per workspace ordered by arrival.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_triage_item_pending
    ON triage_item (workspace_id, first_seen_at)
    WHERE state = 'pending';
