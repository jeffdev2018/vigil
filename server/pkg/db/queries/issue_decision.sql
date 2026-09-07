-- name: CreateIssueDecision :one
INSERT INTO issue_decision (id, workspace_id, issue_id, agent_id, source_task_id, recipient_id, requested_by, requester_type, question, context, options, input_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: GetIssueDecision :one
SELECT * FROM issue_decision WHERE id = $1 AND workspace_id = $2;

-- name: ListIssueDecisionsForRecipient :many
SELECT decision.* FROM issue_decision decision
WHERE decision.workspace_id = $1 AND decision.recipient_id = $2
AND ((decision.status = 'cancelled' OR decision.resume_task_id IS NOT NULL) = sqlc.arg(history)::boolean)
AND (sqlc.narg(before_id)::uuid IS NULL OR (decision.created_at, decision.id) < (
    SELECT cursor.created_at, cursor.id FROM issue_decision cursor
    WHERE cursor.id = sqlc.narg(before_id) AND cursor.workspace_id = $1 AND cursor.recipient_id = $2
))
ORDER BY decision.created_at DESC, decision.id DESC LIMIT 51;

-- name: CountOpenIssueDecisions :one
SELECT count(*) FROM issue_decision WHERE issue_id = $1 AND workspace_id = $2 AND status = 'open';

-- name: AnswerIssueDecision :one
-- Caller holds the issue lock. Response and cancellation are terminal decisions.
UPDATE issue_decision SET status = sqlc.arg(status), answer = sqlc.narg(answer), answered_by = sqlc.arg(answered_by), answered_at = clock_timestamp()
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id) AND status = 'open' RETURNING *;

-- name: SetIssueDecisionResume :one
-- Caller holds the issue lock and inserts the run in this same transaction.
UPDATE issue_decision SET resume_task_id = sqlc.arg(task_id)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id) AND status = 'answered' AND resume_task_id IS NULL RETURNING *;
