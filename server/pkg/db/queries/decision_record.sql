-- Decision memory (K29).

-- name: CreateDecisionRecord :one
INSERT INTO decision_record (id, workspace_id, project_id, issue_id, run_id, source_message_seq, title, context, decision, consequences, author_type, author_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: ListProjectDecisionRecords :many
SELECT sqlc.embed(d), i.number AS issue_number, i.title AS issue_title
FROM decision_record d
JOIN issue i ON i.id = d.issue_id
WHERE d.workspace_id = $1 AND d.project_id = $2
  AND (sqlc.narg(author_type)::text IS NULL OR d.author_type = sqlc.narg(author_type)::text)
ORDER BY d.created_at DESC, d.id DESC
LIMIT 500;

-- name: CountIssueDecisionRecords :one
SELECT COUNT(*) FROM decision_record WHERE issue_id = $1;

-- name: CountRunDecisionRecords :one
SELECT COUNT(*) FROM decision_record WHERE run_id = $1;

-- name: GetLatestCompletedTaskForIssue :one
SELECT * FROM agent_task_queue
WHERE issue_id = $1 AND status = 'completed'
ORDER BY completed_at DESC NULLS LAST, created_at DESC
LIMIT 1;

-- name: GetIssueTask :one
SELECT * FROM agent_task_queue WHERE id = $1 AND issue_id = $2;

-- name: PurgeWorkspaceDecisionRecords :exec
DELETE FROM decision_record WHERE workspace_id = $1;
