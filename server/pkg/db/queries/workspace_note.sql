-- Workspace Brain: notes shared by the whole workspace. Every read carries
-- the workspace_id tenant guard.

-- name: ListWorkspaceNotes :many
-- Brain listing. Optional full-text search (plainto_tsquery over the same
-- expression the GIN index builds), optional single-tag filter, archived rows
-- excluded unless asked for. Pinned notes float to the top so the ones the
-- workspace cares about stay reachable as the list grows.
SELECT * FROM workspace_note
WHERE workspace_id = $1
  AND (sqlc.arg('include_archived')::bool OR archived_at IS NULL)
  AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY(tags))
  AND (
      sqlc.narg('search')::text IS NULL
      OR to_tsvector('simple', title || ' ' || content) @@ plainto_tsquery('simple', sqlc.narg('search')::text)
  )
ORDER BY pinned DESC, updated_at DESC, id DESC
LIMIT sqlc.arg('page_limit')::int;

-- name: GetWorkspaceNote :one
SELECT * FROM workspace_note
WHERE id = $1 AND workspace_id = $2;

-- name: CreateWorkspaceNote :one
INSERT INTO workspace_note (
    id, workspace_id, title, content, tags, source,
    source_task_id, source_agent_id, pinned, created_by_type, created_by_id
)
VALUES (
    sqlc.arg('id'), sqlc.arg('workspace_id'), sqlc.arg('title'), sqlc.arg('content'),
    sqlc.arg('tags'), sqlc.arg('source'), sqlc.narg('source_task_id')::uuid,
    sqlc.narg('source_agent_id')::uuid, sqlc.arg('pinned'),
    sqlc.arg('created_by_type'), sqlc.narg('created_by_id')::uuid
)
RETURNING *;

-- name: UpdateWorkspaceNote :one
-- Optimistic concurrency: the caller sends the revision it read. A stale
-- revision matches no row, which the handler turns into 409 rather than
-- silently overwriting a concurrent edit.
UPDATE workspace_note SET
    title = COALESCE(sqlc.narg('title'), title),
    content = COALESCE(sqlc.narg('content'), content),
    tags = COALESCE(sqlc.narg('tags')::text[], tags),
    pinned = COALESCE(sqlc.narg('pinned'), pinned),
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND revision = sqlc.arg('expected_revision')::bigint
RETURNING *;

-- name: SetWorkspaceNoteArchived :one
-- archive/unarchive. Bumps the revision like any other edit so a client that
-- archived a note cannot then PATCH it with the pre-archive revision.
UPDATE workspace_note SET
    archived_at = sqlc.narg('archived_at')::timestamptz,
    merged_into = sqlc.narg('merged_into')::uuid,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteWorkspaceNote :execrows
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard.
DELETE FROM workspace_note WHERE id = $1 AND workspace_id = $2;

-- name: ListPinnedAndRecentWorkspaceNotesForBrief :many
-- Run-time injection: every pinned note first, then the most recently updated
-- ones, bounded by $2. Archived notes never reach a run.
SELECT * FROM workspace_note
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY pinned DESC, updated_at DESC, id DESC
LIMIT sqlc.arg('note_limit')::int;

-- name: ListWorkspaceNoteTags :many
-- Distinct tags across the workspace's live notes, for the filter chips.
SELECT DISTINCT unnest(tags)::text AS tag
FROM workspace_note
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY tag;

-- name: CountWorkspaceNotesUpdatedSince :one
-- Curation gate: how many live notes changed since the last pass.
SELECT COUNT(*) FROM workspace_note
WHERE workspace_id = $1 AND archived_at IS NULL AND updated_at > $2;

-- name: ListWorkspaceIDsWithNotes :many
-- Curation scope: every workspace that has at least one live note.
SELECT DISTINCT workspace_id FROM workspace_note WHERE archived_at IS NULL;
