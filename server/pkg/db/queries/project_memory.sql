-- name: UpdateProjectMemory :one
UPDATE project
SET memory_rules = $3,
    memory_revision = memory_revision + 1,
    memory_reviewed_by = $4,
    memory_reviewed_at = now(),
    memory_expires_at = sqlc.narg(expires_at),
    memory_source_review = sqlc.narg(memory_source_review),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND memory_revision = $5
RETURNING *;

-- name: SaveProjectMemoryVersion :exec
INSERT INTO project_memory_version (project_id, workspace_id, revision, rules, reviewed_by, reviewed_at, expires_at, restored_from_revision, source_review)
SELECT p.id, p.workspace_id, p.memory_revision, p.memory_rules, p.memory_reviewed_by, p.memory_reviewed_at, p.memory_expires_at, sqlc.narg(restored_from_revision)::integer, p.memory_source_review
FROM project p WHERE p.id = sqlc.arg(project_id) AND p.workspace_id = sqlc.arg(workspace_id)
ON CONFLICT (project_id, revision) DO NOTHING;

-- name: GetProjectMemoryVersion :one
SELECT * FROM project_memory_version WHERE project_id = $1 AND workspace_id = $2 AND revision = $3;

-- name: ListProjectMemoryVersions :many
SELECT * FROM project_memory_version WHERE project_id = $1 AND workspace_id = $2 AND revision < $3
ORDER BY revision DESC LIMIT 21;

-- name: DeleteProjectMemoryVersions :exec
DELETE FROM project_memory_version WHERE project_id = $1 AND workspace_id = $2;

-- name: GetProjectMemoryVersionForCorrection :one
SELECT * FROM project_memory_version
WHERE project_id = $1 AND workspace_id = $2 AND source_review->>'review_id' = sqlc.arg(review_id)::text
ORDER BY revision DESC
LIMIT 1;

-- name: GetProjectMemoryCorrectionSource :one
SELECT review.* FROM issue_delivery_review review
JOIN issue ON issue.id = review.issue_id AND issue.workspace_id = review.workspace_id
JOIN agent_task_queue task ON task.id = review.task_id AND task.issue_id = issue.id
WHERE review.id = $1 AND review.workspace_id = $2 AND issue.project_id = $3
AND task.chat_session_id IS NULL;
