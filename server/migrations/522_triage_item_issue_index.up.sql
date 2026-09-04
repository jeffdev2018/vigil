-- Resolution lookup: find the item an accepted issue came from (and the
-- items merged into it), without scanning the queue.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_triage_item_issue
    ON triage_item (issue_id)
    WHERE issue_id IS NOT NULL;
