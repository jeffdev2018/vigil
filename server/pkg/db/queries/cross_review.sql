-- Cross-provider self-review (K15).

-- name: SetTaskReviewOf :one
UPDATE agent_task_queue SET review_of_task_id = $2 WHERE id = $1 RETURNING *;

-- name: ListCrossReviewCandidates :many
-- Agents on a provider other than the author's, least recently used as a
-- reviewer first, so the load spreads without configuration.
SELECT a.id, a.name, r.provider,
       (SELECT MAX(t.created_at) FROM agent_task_queue t WHERE t.agent_id = a.id AND t.review_of_task_id IS NOT NULL) AS last_review_at
FROM agent a
JOIN agent_runtime r ON r.id = a.runtime_id
WHERE a.workspace_id = $1 AND a.kind = 'user' AND a.archived_at IS NULL AND r.provider <> '' AND r.provider <> sqlc.arg(author_provider)::text
ORDER BY last_review_at NULLS FIRST, a.created_at;

-- name: ListCrossReviewsForIssue :many
SELECT t.id, t.review_of_task_id, t.agent_id, t.status, t.created_at, t.completed_at, a.name AS reviewer_name, COALESCE(r.provider, '') AS reviewer_provider
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
LEFT JOIN agent_runtime r ON r.id = t.runtime_id
WHERE t.issue_id = $1 AND t.review_of_task_id IS NOT NULL
ORDER BY t.created_at DESC;

-- name: GetLatestCrossReviewForTask :one
SELECT * FROM agent_task_queue WHERE review_of_task_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: GetLatestReviewReportMessage :one
SELECT * FROM task_message WHERE task_id = $1 AND type = 'review_report' ORDER BY seq DESC LIMIT 1;
