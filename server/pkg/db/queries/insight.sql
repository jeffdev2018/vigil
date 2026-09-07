-- name: ListInsightWidgets :many
-- Everything this member may see: their own widgets plus the ones shared with
-- the workspace. Both halves carry workspace_id, so a widget from another
-- workspace is unreachable whatever the caller sends.
SELECT * FROM insight_widget
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (owner_id = sqlc.arg('viewer_id')::uuid OR visibility = 'workspace')
ORDER BY position ASC, created_at ASC;

-- name: GetInsightWidget :one
SELECT * FROM insight_widget
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CreateInsightWidget :one
INSERT INTO insight_widget (
    workspace_id, owner_id, name, question, query, display, visibility, position
)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('owner_id')::uuid,
    sqlc.arg('name'),
    sqlc.arg('question'),
    sqlc.arg('query')::jsonb,
    sqlc.arg('display')::jsonb,
    sqlc.arg('visibility'),
    COALESCE(
        (SELECT MAX(position) + 1 FROM insight_widget
          WHERE workspace_id = sqlc.arg('workspace_id')::uuid),
        0
    )
)
RETURNING *;

-- name: UpdateInsightWidget :one
-- Optimistic concurrency: the WHERE clause carries the revision the client
-- read, so a second concurrent PATCH matches no row and the handler answers
-- 409 instead of overwriting the first one's edit.
UPDATE insight_widget
SET name       = COALESCE(sqlc.narg('name'), name),
    question   = COALESCE(sqlc.narg('question'), question),
    query      = COALESCE(sqlc.narg('query')::jsonb, query),
    display    = COALESCE(sqlc.narg('display')::jsonb, display),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    position   = COALESCE(sqlc.narg('position')::double precision, position),
    revision   = revision + 1,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND revision = sqlc.arg('expected_revision')::int
RETURNING *;

-- name: DeleteInsightWidget :execrows
DELETE FROM insight_widget
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CreateInsightQueryLog :exec
-- Fire-and-forget: a failed log write must never fail the answer the user is
-- waiting for, so the handler ignores the error and logs it.
INSERT INTO insight_query_log (
    workspace_id, user_id, question, compiled, outcome, duration_ms
)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.narg('user_id')::uuid,
    sqlc.arg('question'),
    sqlc.narg('compiled')::jsonb,
    sqlc.arg('outcome'),
    sqlc.arg('duration_ms')
);
