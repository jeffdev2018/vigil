-- Decision Cards (K01).

-- name: CreateIssueDecision :one
INSERT INTO issue_decision (workspace_id, issue_id, task_id, asked_by_type, asked_by_id, question, options, recommended_option_id, urgency)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetIssueDecision :one
SELECT * FROM issue_decision WHERE id = $1 AND issue_id = $2;

-- name: ListIssueDecisions :many
SELECT * FROM issue_decision
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY (response IS NULL) DESC, created_at DESC
LIMIT 50;

-- name: CountPendingIssueDecisions :one
SELECT COUNT(*)::bigint FROM issue_decision WHERE issue_id = $1 AND response IS NULL;

-- name: RespondIssueDecision :one
-- Idempotent guard: a second answer matches no row.
UPDATE issue_decision
SET response = $2, responded_by_type = $3, responded_by_id = $4, responded_at = now()
WHERE id = $1 AND response IS NULL
RETURNING *;

-- name: SetIssueDecisionResumeTask :exec
UPDATE issue_decision SET resume_task_id = $2 WHERE id = $1;
