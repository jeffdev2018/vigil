-- Comment threads anchored to a diff line (F07 / JEF-21). A thread can point
-- at a file, a line range and a side of ONE revision of a linked pull
-- request's head, optionally at the review flag (F06) that raised it.
--
-- The anchor lives on the thread ROOT only. Replies inherit it at read time,
-- which is why there is no CHECK tying these columns to parent_id: the rule is
-- "a reply may not carry its own anchor", and that is a handler decision about
-- a request, not an invariant the database can state about a row. A CHECK here
-- would also make any future re-parenting of an anchored thread fail at the
-- storage layer rather than where the product rule lives.
--
-- anchor_kind is free text on purpose. 'diff_line' is the only value v1
-- writes; a newer server adding a kind must not make this build hide the
-- thread, so readers treat an unknown kind as "render without the anchor".
-- Every column is nullable and NULL anchor_kind means "not anchored", so the
-- whole set is additive: existing comments are untouched.
--
-- No foreign keys by repository convention: anchor_pr_id points at
-- github_pull_request or vcs_pull_request depending on anchor_pr_source, and
-- anchor_review_flag_id at review_flag. All three are validated in the handler.
ALTER TABLE comment
    ADD COLUMN IF NOT EXISTS anchor_kind            TEXT,
    ADD COLUMN IF NOT EXISTS anchor_pr_source       TEXT,
    ADD COLUMN IF NOT EXISTS anchor_pr_id           UUID,
    -- The head the line range belongs to. Part of the anchor's meaning, not
    -- metadata: the same line number means something else on another head,
    -- and a thread whose head is behind the pull request's is shown as stale.
    ADD COLUMN IF NOT EXISTS anchor_head_sha        TEXT,
    ADD COLUMN IF NOT EXISTS anchor_file_path       TEXT,
    ADD COLUMN IF NOT EXISTS anchor_line_start      INT,
    ADD COLUMN IF NOT EXISTS anchor_line_end        INT,
    -- 'new' is the changed code; 'old' lets a thread point at a deletion.
    ADD COLUMN IF NOT EXISTS anchor_side            TEXT,
    ADD COLUMN IF NOT EXISTS anchor_review_flag_id  UUID;
