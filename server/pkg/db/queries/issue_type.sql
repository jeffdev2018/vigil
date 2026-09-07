-- Work item type catalogue (F30). Each workspace holds the 4 seeded system
-- types plus any custom ones. `issue.issue_type` stores a key from here, or
-- NULL for an untyped issue.

-- name: SeedIssueTypeEntries :exec
-- Idempotent seed of the 4 system types. Safe to call concurrently from
-- multiple pods during a rolling deploy: the unique (workspace_id, key) index
-- makes a losing racer a no-op rather than an error.
INSERT INTO issue_type (workspace_id, key, name, description, color, icon, is_system, position)
VALUES
    (sqlc.arg('workspace_id')::uuid, 'bug', 'Bug', 'Something is broken and has to be fixed.', '#ef4444', 'bug', TRUE, 0),
    (sqlc.arg('workspace_id')::uuid, 'story', 'Story', 'A change that delivers user-visible value.', '#3b82f6', 'bookmark', TRUE, 1),
    (sqlc.arg('workspace_id')::uuid, 'epic', 'Epic', 'A body of work that other issues roll up into.', '#a855f7', 'layers-3', TRUE, 2),
    (sqlc.arg('workspace_id')::uuid, 'task', 'Task', 'A unit of work with no user-facing story behind it.', '#6b7280', 'list-checks', TRUE, 3)
ON CONFLICT DO NOTHING;

-- name: ListIssueTypeEntries :many
-- Display order: position, then key as a stable tiebreak. The seed positions
-- the four system types 0..3 so a workspace that never opens the settings page
-- always sees bug / story / epic / task in that order.
SELECT * FROM issue_type
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.arg('include_archived')::bool OR archived_at IS NULL)
ORDER BY position, key;

-- name: GetIssueTypeEntryByKey :one
SELECT * FROM issue_type
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND key = sqlc.arg('key')::text;

-- name: GetIssueTypeEntryByID :one
SELECT * FROM issue_type
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CreateIssueTypeEntry :one
-- Custom types only: is_system is never set here, so the non-archivable CHECK
-- can only ever apply to seeded rows.
INSERT INTO issue_type (workspace_id, key, name, description, color, icon, position)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('key')::text,
    sqlc.arg('name')::text,
    sqlc.arg('description')::text,
    sqlc.arg('color')::text,
    sqlc.arg('icon')::text,
    COALESCE(
        (SELECT MAX(position) + 1 FROM issue_type
         WHERE workspace_id = sqlc.arg('workspace_id')::uuid),
        0
    )
)
RETURNING *;

-- name: UpdateIssueTypeEntry :one
-- key is absent on purpose: it is immutable after create, because it is the
-- value stored in issue.issue_type and referenced by property scopes.
--
-- Unlike a status, a SYSTEM type is editable here — its name, color and icon
-- carry no platform behavior, so letting a workspace rename "Story" to
-- "Feature" costs nothing and is the single most-asked-for tweak. Archiving is
-- what stays refused.
UPDATE issue_type SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    icon = COALESCE(sqlc.narg('icon'), icon),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND archived_at IS NULL
RETURNING *;

-- name: ArchiveIssueTypeEntry :one
-- System types are excluded by is_system: the four seeded handles are what the
-- API docs and agent instructions reference.
--
-- Archiving retires a type from FUTURE assignment only. Issues already on it
-- keep it and keep resolving their name/color/icon through this row.
UPDATE issue_type SET
    archived_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND is_system = FALSE
  AND archived_at IS NULL
RETURNING *;

-- name: CountIssuesUsingIssueType :one
SELECT COUNT(*)::bigint FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND issue_type = sqlc.arg('key')::text;

-- name: DeleteIssueTypeEntriesForWorkspace :exec
-- No foreign keys by project rule, so workspace teardown cleans up here.
DELETE FROM issue_type WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: LockIssueTypeCatalog :exec
-- EXCLUSIVE transaction lock over a workspace's type catalogue, held by create
-- (which derives keys from the existing set) and by archive.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('workspace_id')::uuid::text || ':issue_type', 0));

-- name: ListActiveCustomIssueTypeEntries :many
-- The exact set a reorder must cover, read inside the reorder transaction.
SELECT * FROM issue_type
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND archived_at IS NULL
ORDER BY position, key;

-- name: ReorderIssueTypeEntries :execrows
-- Atomic reorder over the whole catalogue. One statement, so a failure leaves
-- the order untouched instead of committing the partially-applied prefix a
-- per-row PATCH loop produces.
--
-- System rows ARE included, unlike the status reorder: a type carries no
-- behavior, so "put Bug last" is a legitimate thing to want.
UPDATE issue_type t
SET position = v.ordinality::int,
    updated_at = now()
FROM unnest(sqlc.arg('ids')::uuid[]) WITH ORDINALITY AS v(id, ordinality)
WHERE t.id = v.id
  AND t.workspace_id = sqlc.arg('workspace_id')::uuid
  AND t.archived_at IS NULL;

-- name: SetIssueIssueType :exec
-- Mirrors SetIssueCycle / SetIssueGoal (F29 / K74): the type is written on its
-- own rather than through UpdateIssue, so no existing UpdateIssueParams builder
-- has to learn to pre-fill a new bare narg — which would silently clear it on
-- every write that does not mention the type.
UPDATE issue SET issue_type = sqlc.narg('issue_type'), updated_at = now()
WHERE id = $1 AND workspace_id = $2;
