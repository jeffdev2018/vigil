-- Cross-provider self-review (K15).

-- name: SetTaskReviewOf :one
UPDATE agent_task_queue SET review_of_task_id = $2 WHERE id = $1 RETURNING *;

-- name: ListCrossReviewCandidates :many
-- Agents of the workspace that are not the author and do not run the same
-- (runtime, model) pair as the author — the same pair would be the same
-- reviewer wearing another name (JEF-238). A different provider is preferred;
-- least recently used as a reviewer first, so the load spreads without
-- configuration.
SELECT a.id, a.name, r.provider,
       (SELECT MAX(t.created_at) FROM agent_task_queue t WHERE t.agent_id = a.id AND t.review_of_task_id IS NOT NULL) AS last_review_at
FROM agent a
JOIN agent_runtime r ON r.id = a.runtime_id
WHERE a.workspace_id = $1 AND a.kind = 'user' AND a.archived_at IS NULL AND r.provider <> ''
  AND a.id <> sqlc.arg(author_agent_id)::uuid
  AND (
    sqlc.arg(author_runtime_id)::uuid IS NULL
    OR a.runtime_id IS DISTINCT FROM sqlc.arg(author_runtime_id)::uuid
    OR COALESCE(a.model, '') <> COALESCE(sqlc.arg(author_model)::text, '')
  )
ORDER BY (r.provider <> sqlc.arg(author_provider)::text) DESC, last_review_at NULLS FIRST, a.created_at;

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

-- name: GetLatestCompletedWorkerTaskForIssue :one
-- The most recent completed run that is not itself a review: the run whose
-- review verdict the done gate (JEF-238) consults.
SELECT * FROM agent_task_queue
WHERE issue_id = $1 AND review_of_task_id IS NULL AND status = 'completed'
ORDER BY completed_at DESC NULLS LAST, created_at DESC
LIMIT 1;

-- name: CountCrossReviewsForIssue :one
-- Review runs of the issue; review tasks live on the same issue as the run
-- they review, so their review_of_task_id always points at a task of $1.
SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND review_of_task_id IS NOT NULL;
