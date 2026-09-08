-- Review flags by severity (F06 / JEF-19).

-- name: CreateReviewFlag :one
INSERT INTO review_flag (
    id, workspace_id, issue_id, pr_source, pr_id, head_sha,
    file_path, line_start, line_end, side, severity, confidence,
    title, body, author_agent_id, author_user_id, task_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
)
RETURNING *;

-- ListReviewFlagsByIssue is the authoritative order, and it lives here rather
-- than in the client: severity first because a bug outranks any amount of
-- confidence in a warning, then confidence descending with "did not say"
-- last, then the file and the line so two equally-ranked flags read in the
-- order a reviewer would walk the diff.
--
-- It returns every state and the handler filters. The open counts have to be
-- computed over the whole set whatever the requested filter is, so a second
-- state-filtered query would only buy a second round trip.
-- name: ListReviewFlagsByIssue :many
SELECT * FROM review_flag
WHERE issue_id = $1
ORDER BY
    CASE severity WHEN 'bug' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,
    confidence DESC NULLS LAST,
    file_path,
    line_start,
    created_at;

-- name: GetReviewFlag :one
SELECT * FROM review_flag WHERE id = $1;

-- SetReviewFlagState settles or reopens one flag. Author, head_sha and the
-- range are never touched: the flag stays the record of what was found, on
-- which revision, by whom. Reopening clears the resolver rather than keeping
-- a stale one.
-- name: SetReviewFlagState :one
UPDATE review_flag
SET state = $2,
    resolved_by_type = $3,
    resolved_by_id = $4,
    resolved_at = CASE WHEN $2 = 'open' THEN NULL ELSE now() END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountReviewFlagsForTask :one
SELECT count(*) FROM review_flag WHERE task_id = $1;

-- StaleReviewFlagsForMovedHead marks every open flag written against a head
-- the pull request has moved past. Never a delete: the finding was true of
-- the code it was written against.
-- name: StaleReviewFlagsForMovedHead :exec
UPDATE review_flag
SET state = 'stale', updated_at = now()
WHERE pr_id = $1 AND head_sha <> $2 AND state = 'open';

-- name: PurgeWorkspaceReviewFlags :exec
DELETE FROM review_flag WHERE workspace_id = $1;
