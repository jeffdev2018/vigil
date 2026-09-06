-- The reverse lookup: opening a mirror issue asks "which source am I a
-- mirror of?" so the detail page can show its origin banner.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_mirror_mirror_issue
    ON issue_mirror (mirror_issue_id);
