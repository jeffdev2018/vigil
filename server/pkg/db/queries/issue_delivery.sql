-- name: GetIssueDeliveryContract :one
SELECT * FROM issue_delivery_contract WHERE issue_id = $1 AND workspace_id = $2;

-- name: SaveIssueDeliveryContract :one
-- The caller holds the issue row lock and checks the expected revision.
INSERT INTO issue_delivery_contract (issue_id, workspace_id, criteria, revision, updated_by)
VALUES ($1, $2, $3, 1, $4)
ON CONFLICT (issue_id) DO UPDATE
SET criteria = EXCLUDED.criteria, revision = issue_delivery_contract.revision + 1,
    updated_by = EXCLUDED.updated_by, updated_at = now()
WHERE issue_delivery_contract.workspace_id = EXCLUDED.workspace_id
RETURNING *;

-- name: SetIssueDeliveryCorrectionTask :one
-- Caller holds the issue lock and inserts the task in this same transaction.
UPDATE issue_delivery_review SET correction_task_id = sqlc.arg(task_id)
WHERE id = sqlc.arg(id) AND issue_id = sqlc.arg(issue_id)
    AND workspace_id = sqlc.arg(workspace_id) AND decision = 'changes_requested'
    AND correction_task_id IS NULL
RETURNING *;

-- name: GetLatestIssueDeliveryTask :one
SELECT task.* FROM agent_task_queue task
JOIN issue ON issue.id = task.issue_id
WHERE issue.id = $1 AND issue.workspace_id = $2 AND task.chat_session_id IS NULL
ORDER BY task.created_at DESC, task.id DESC LIMIT 1;

-- name: ListIssueDeliveryReviews :many
SELECT * FROM issue_delivery_review WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at DESC, id DESC LIMIT 50;

-- name: GetIssueDeliveryReview :one
SELECT * FROM issue_delivery_review WHERE id = $1 AND issue_id = $2 AND workspace_id = $3;

-- name: CreateIssueDeliveryReview :one
INSERT INTO issue_delivery_review (
    id, issue_id, workspace_id, task_id, decision, feedback, assessments,
    snapshot, snapshot_token, input_hash, reviewed_by, usage_snapshot, human_effort_seconds
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: ListIssueDeliveryUsage :many
-- One snapshot of every visible run, including those with no usage report.
SELECT task.id AS task_id, task.status, usage.task_id AS usage_task_id,
    COALESCE(usage.provider, '')::text AS provider, COALESCE(usage.model, '')::text AS model,
    COALESCE(usage.input_tokens, 0)::bigint AS input_tokens,
    COALESCE(usage.output_tokens, 0)::bigint AS output_tokens,
    COALESCE(usage.cache_read_tokens, 0)::bigint AS cache_read_tokens,
    COALESCE(usage.cache_write_tokens, 0)::bigint AS cache_write_tokens,
    usage.cost_usd_ticks
FROM agent_task_queue task JOIN issue ON issue.id = task.issue_id
LEFT JOIN task_usage usage ON usage.task_id = task.id
WHERE issue.id = $1 AND issue.workspace_id = $2 AND task.chat_session_id IS NULL
ORDER BY task.created_at, task.id, usage.provider, usage.model;

-- name: ListIssueDeliveryReviewPage :many
SELECT review.* FROM issue_delivery_review review
WHERE review.issue_id = $1 AND review.workspace_id = $2
AND (sqlc.narg(before_id)::uuid IS NULL OR (review.created_at, review.id) < (
    SELECT cursor.created_at, cursor.id FROM issue_delivery_review cursor
    WHERE cursor.id = sqlc.narg(before_id) AND cursor.issue_id = $1 AND cursor.workspace_id = $2
))
ORDER BY review.created_at DESC, review.id DESC LIMIT 21;

-- name: GetIssueDeliveryMetrics :one
WITH ordered AS (
    SELECT decision, snapshot_token,
        lag(decision) OVER (ORDER BY created_at, id) AS previous_decision,
        row_number() OVER (PARTITION BY snapshot_token ORDER BY created_at DESC, id DESC) AS result_rank
    FROM issue_delivery_review WHERE issue_id = $1 AND workspace_id = $2
)
SELECT count(*)::bigint AS review_count,
    count(*) FILTER (WHERE result_rank = 1)::bigint AS reviewed_results,
    count(*) FILTER (WHERE result_rank = 1 AND decision = 'accepted')::bigint AS accepted_results,
    count(*) FILTER (WHERE decision = 'changes_requested')::bigint AS correction_requests,
    count(*) FILTER (WHERE decision = 'changes_requested' AND previous_decision = 'accepted')::bigint AS acceptance_reversals
FROM ordered;
