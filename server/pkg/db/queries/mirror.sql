-- Cross-repo mirror issues (K54).
--
-- project_mirror_link is the configuration: an issue of source_project_id
-- carrying trigger_label gets mirrored into target_project_id. issue_mirror is
-- the result: one row per mirror issue actually produced, which is what makes
-- the trigger idempotent and what the issue detail page reads back.

-- name: CreateProjectMirrorLink :one
INSERT INTO project_mirror_link (workspace_id, source_project_id, target_project_id, trigger_label, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetProjectMirrorLink :one
SELECT * FROM project_mirror_link
WHERE id = $1 AND workspace_id = $2;

-- name: ListProjectMirrorLinksBySource :many
-- Links configured on one source project, with the target project's title so
-- the settings section renders without an N+1 lookup. A link whose target
-- project was deleted still lists (LEFT JOIN), with an empty title, rather
-- than vanishing silently — there are no foreign keys to clean it up.
SELECT l.*, COALESCE(p.title, '')::text AS target_project_title
FROM project_mirror_link l
LEFT JOIN project p ON p.id = l.target_project_id
WHERE l.workspace_id = $1 AND l.source_project_id = $2
ORDER BY l.created_at ASC;

-- name: ListProjectMirrorLinksByWorkspace :many
-- Every link of the workspace, used by the cycle guard: it needs the whole
-- source -> target graph, not one project's slice.
SELECT * FROM project_mirror_link
WHERE workspace_id = $1;

-- name: ListMirrorLinksForLabel :many
-- The trigger lookup. Label matching is case-insensitive so "Mirror" attached
-- by a person fires a link configured as "mirror".
SELECT * FROM project_mirror_link
WHERE workspace_id = $1
  AND source_project_id = $2
  AND LOWER(trigger_label) = LOWER(sqlc.arg('trigger_label')::text)
ORDER BY created_at ASC;

-- name: DeleteProjectMirrorLink :one
-- :one RETURNING id so the handler tells pgx.ErrNoRows (404) from an
-- infrastructure error (500). Mirrors already produced are deliberately kept:
-- they are real issues someone may be working on.
DELETE FROM project_mirror_link
WHERE id = $1 AND workspace_id = $2
RETURNING id;

-- name: CreateIssueMirror :one
-- ON CONFLICT DO NOTHING against uq_issue_mirror_pair, so a concurrent double
-- attach cannot record the same pair twice. :one with no row returned means
-- "already recorded"; the caller reads pgx.ErrNoRows as success.
INSERT INTO issue_mirror (workspace_id, source_issue_id, mirror_issue_id, link_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (source_issue_id, mirror_issue_id) DO NOTHING
RETURNING *;

-- name: GetIssueMirror :one
SELECT * FROM issue_mirror
WHERE id = $1 AND workspace_id = $2;

-- name: ListIssueMirrorsBySource :many
-- The mirrors of a source issue, with everything the chip needs: the mirror
-- issue's number, title, status and project title.
SELECT m.*,
       i.number       AS mirror_number,
       i.title        AS mirror_title,
       i.status       AS mirror_status,
       i.project_id   AS mirror_project_id,
       COALESCE(p.title, '')::text AS mirror_project_title
FROM issue_mirror m
JOIN issue i ON i.id = m.mirror_issue_id
LEFT JOIN project p ON p.id = i.project_id
WHERE m.workspace_id = $1 AND m.source_issue_id = $2
ORDER BY m.created_at ASC;

-- name: GetIssueMirrorByMirrorIssue :one
-- The reverse: "which source is this issue a mirror of?" A mirror is produced
-- by exactly one link, so this is at most one row.
SELECT m.*,
       i.number       AS source_number,
       i.title        AS source_title,
       i.status       AS source_status,
       i.project_id   AS source_project_id,
       COALESCE(p.title, '')::text AS source_project_title
FROM issue_mirror m
JOIN issue i ON i.id = m.source_issue_id
LEFT JOIN project p ON p.id = i.project_id
WHERE m.workspace_id = $1 AND m.mirror_issue_id = $2
LIMIT 1;

-- name: ListIssueMirrorsForSourceAndLink :many
-- Idempotency check: has this source already been mirrored through this link?
SELECT * FROM issue_mirror
WHERE source_issue_id = $1 AND link_id = $2;

-- name: SetIssueMirrorTypeSynced :one
UPDATE issue_mirror
SET type_synced = sqlc.arg('type_synced')::boolean
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: PurgeWorkspaceIssueMirrors :exec
DELETE FROM issue_mirror WHERE workspace_id = $1;

-- name: PurgeWorkspaceProjectMirrorLinks :exec
DELETE FROM project_mirror_link WHERE workspace_id = $1;
