ALTER TABLE comment
    DROP COLUMN IF EXISTS anchor_kind,
    DROP COLUMN IF EXISTS anchor_pr_source,
    DROP COLUMN IF EXISTS anchor_pr_id,
    DROP COLUMN IF EXISTS anchor_head_sha,
    DROP COLUMN IF EXISTS anchor_file_path,
    DROP COLUMN IF EXISTS anchor_line_start,
    DROP COLUMN IF EXISTS anchor_line_end,
    DROP COLUMN IF EXISTS anchor_side,
    DROP COLUMN IF EXISTS anchor_review_flag_id;
