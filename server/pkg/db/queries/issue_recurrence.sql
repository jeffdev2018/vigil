-- name: CreateIssueRecurrence :one
INSERT INTO issue_recurrence (id, workspace_id, issue_id, cron_expression, timezone, mode, enabled, next_run_at, created_by_type, created_by_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, sqlc.narg('next_run_at'), $8, sqlc.narg('created_by_id'))
RETURNING *;

-- name: GetIssueRecurrence :one
SELECT * FROM issue_recurrence WHERE id = $1 AND workspace_id = $2;

-- name: GetIssueRecurrenceByIssue :one
-- The rule an issue belongs to: as the source, or as an occurrence.
SELECT r.* FROM issue_recurrence r
WHERE r.workspace_id = sqlc.arg('workspace_id')
  AND (r.issue_id = sqlc.arg('issue_id') OR r.id = (SELECT recurrence_id FROM issue WHERE id = sqlc.arg('issue_id')))
LIMIT 1;

-- name: UpdateIssueRecurrence :one
UPDATE issue_recurrence
SET cron_expression = $3, timezone = $4, mode = $5, enabled = $6, next_run_at = sqlc.narg('next_run_at'), updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteIssueRecurrence :execrows
DELETE FROM issue_recurrence WHERE id = $1 AND workspace_id = $2;

-- name: DeleteIssueRecurrenceByID :exec
DELETE FROM issue_recurrence WHERE id = $1;

-- name: ClearIssueRecurrenceLinks :exec
UPDATE issue SET recurrence_id = NULL WHERE recurrence_id = $1;

-- name: SetIssueRecurrenceLink :exec
UPDATE issue SET recurrence_id = $2 WHERE id = $1;

-- name: AdvanceIssueRecurrence :one
UPDATE issue_recurrence
SET last_occurrence_id = $2, occurrence_count = occurrence_count + 1, next_run_at = sqlc.narg('next_run_at'), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListDueIssueRecurrences :many
SELECT * FROM issue_recurrence
WHERE enabled AND mode = 'schedule' AND next_run_at IS NOT NULL AND next_run_at <= $1
ORDER BY next_run_at ASC
LIMIT $2;

-- name: ListOnCloseIssueRecurrences :many
-- Rules whose current occurrence (the last one, else the source) is closed.
-- The category is resolved in Go from the status key.
SELECT r.*, i.status AS current_status
FROM issue_recurrence r
JOIN issue i ON i.id = COALESCE(r.last_occurrence_id, r.issue_id)
WHERE r.enabled AND r.mode = 'on_close'
ORDER BY r.updated_at ASC
LIMIT $1;

-- name: ListIssueRecurrenceOccurrences :many
SELECT id, number, title, status, created_at, due_date
FROM issue
WHERE recurrence_id = $1 AND workspace_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: PurgeWorkspaceIssueRecurrences :exec
DELETE FROM issue_recurrence WHERE workspace_id = $1;
