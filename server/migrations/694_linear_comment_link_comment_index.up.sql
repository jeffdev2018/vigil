-- Outbound loop guard: before pushing a Multica comment to Linear we ask
-- whether that comment is itself a mirror of one.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_linear_comment_link_comment
    ON linear_comment_link (comment_id);
