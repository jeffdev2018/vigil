-- Workspace doctrine (OS plan, chantier 22). The live text lives in
-- workspace.context; these queries own the revision ledger and the reports.

-- name: GetWorkspaceDoctrine :one
SELECT id, context, doctrine_revision, doctrine_updated_at, doctrine_updated_by, settings
FROM workspace WHERE id = $1;

-- name: LockWorkspaceForDoctrine :one
SELECT id, context, doctrine_revision, settings FROM workspace WHERE id = $1 FOR UPDATE;

-- name: ActivateWorkspaceDoctrine :exec
UPDATE workspace SET context = $2, doctrine_revision = $3, doctrine_updated_at = now(), doctrine_updated_by = $4, updated_at = now()
WHERE id = $1;

-- name: SupersedeActiveDoctrineVersion :exec
UPDATE workspace_doctrine_version SET status = 'superseded' WHERE workspace_id = $1 AND status = 'active';

-- name: CreateDoctrineVersion :one
INSERT INTO workspace_doctrine_version (workspace_id, revision, content, status, note, author_id, reviewed_by, reviewed_at, review_note, restored_from_revision)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ReviewDoctrineVersion :one
UPDATE workspace_doctrine_version SET status = $3, revision = $4, reviewed_by = $5, reviewed_at = now(), review_note = $6
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
RETURNING *;

-- name: GetDoctrineVersion :one
SELECT * FROM workspace_doctrine_version WHERE id = $1 AND workspace_id = $2;

-- name: GetDoctrineVersionByRevision :one
SELECT * FROM workspace_doctrine_version WHERE workspace_id = $1 AND revision = $2;

-- name: GetActiveDoctrineVersion :one
SELECT * FROM workspace_doctrine_version WHERE workspace_id = $1 AND status = 'active';

-- name: GetPendingDoctrineVersion :one
SELECT * FROM workspace_doctrine_version WHERE workspace_id = $1 AND status = 'pending';

-- name: ListDoctrineVersions :many
-- Exclusive keyset cursor on (created_at, id) so new versions never shift older pages.
SELECT * FROM workspace_doctrine_version
WHERE workspace_id = $1
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL
       OR created_at < sqlc.narg('before_created_at')::timestamptz
       OR (created_at = sqlc.narg('before_created_at')::timestamptz AND id < sqlc.narg('before_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: CreateDoctrineReport :one
INSERT INTO workspace_doctrine_report (id, workspace_id, doctrine_revision, kind, summary, passage, reporter_type, reporter_id, task_id, issue_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetDoctrineReport :one
SELECT * FROM workspace_doctrine_report WHERE id = $1 AND workspace_id = $2;

-- name: ListDoctrineReports :many
SELECT * FROM workspace_doctrine_report
WHERE workspace_id = $1 AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: CountOpenDoctrineReports :one
SELECT count(*) FROM workspace_doctrine_report WHERE workspace_id = $1 AND status = 'open';

-- name: ResolveDoctrineReport :one
UPDATE workspace_doctrine_report SET status = $3, resolved_by = $4, resolved_at = now(), resolution_note = $5
WHERE id = $1 AND workspace_id = $2 AND status = 'open'
RETURNING *;

-- name: PurgeWorkspaceDoctrineVersions :exec
DELETE FROM workspace_doctrine_version WHERE workspace_id = $1;

-- name: PurgeWorkspaceDoctrineReports :exec
DELETE FROM workspace_doctrine_report WHERE workspace_id = $1;
