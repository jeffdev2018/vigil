-- Decision Cards (K01).

-- name: CreateIssueDecision :one
INSERT INTO issue_decision (workspace_id, issue_id, task_id, asked_by_type, asked_by_id, question, options, recommended_option_id, urgency, plan_version, interview_group_id, interview_position, interview_resume_status, sla_deadline_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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

-- Requirement Interview (K13).

-- name: ListInterviewGroup :many
SELECT * FROM issue_decision
WHERE interview_group_id = $1 AND issue_id = $2
ORDER BY interview_position ASC;

-- name: CountPendingInterviewQuestions :one
SELECT COUNT(*)::bigint FROM issue_decision
WHERE issue_id = $1 AND interview_group_id IS NOT NULL AND response IS NULL;

-- Decision SLA (K35).

-- name: ListOverdueIssueDecisions :many
SELECT * FROM issue_decision
WHERE response IS NULL AND sla_deadline_at IS NOT NULL AND sla_deadline_at <= $1 AND escalation_level < 2
ORDER BY sla_deadline_at ASC
LIMIT 200;

-- name: EscalateIssueDecision :one
-- The level predicate makes a concurrent tick a no-op instead of a double step.
UPDATE issue_decision
SET escalation_level = $2, escalated_at = $3, sla_deadline_at = $4
WHERE id = $1 AND response IS NULL AND escalation_level = $2 - 1
RETURNING *;

-- name: ListIssueDecisionsForTask :many
-- Replay (K70): the decisions one run asked, oldest first.
SELECT * FROM issue_decision WHERE task_id = $1 ORDER BY created_at ASC, id ASC;
