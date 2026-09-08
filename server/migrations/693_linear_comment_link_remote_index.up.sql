-- Inbound dedup: a redelivered Linear comment event finds its row here.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_linear_comment_link_remote
    ON linear_comment_link (linear_comment_id);
