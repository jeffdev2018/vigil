-- The head-move hook stales every open flag of a pull request written against
-- a head that is no longer current, on every push to every linked PR.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_review_flag_pr_head
    ON review_flag (pr_id, head_sha);
