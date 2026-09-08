-- The anchored-threads read asks for one pull request's threads, usually
-- scoped to one head. Partial on anchor_kind so the index only holds the
-- anchored rows — a tiny fraction of a hot table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_anchor_pr_head
    ON comment (anchor_pr_id, anchor_head_sha)
    WHERE anchor_kind IS NOT NULL;
