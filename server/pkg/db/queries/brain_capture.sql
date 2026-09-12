-- Brain capture (OS plan, vague B): the raw inbox and its organization into
-- notes. Ranked note search lives in brain_search.sql.

-- name: CreateBrainCapture :one
INSERT INTO brain_capture (id, workspace_id, kind, content, url, title_hint, attachment_id, origin, transcription_status, created_by_type, created_by_id, source_task_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetBrainCapture :one
SELECT * FROM brain_capture WHERE id = $1 AND workspace_id = $2;

-- name: ListBrainCaptures :many
SELECT * FROM brain_capture
WHERE workspace_id = $1 AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: CountRawBrainCaptures :one
SELECT count(*) FROM brain_capture WHERE workspace_id = $1 AND status = 'raw';

-- name: SetBrainCaptureSuggestion :one
UPDATE brain_capture SET suggestion = $3, updated_at = now() WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: SetBrainCaptureTranscript :one
UPDATE brain_capture SET content = $3, transcription_status = $4, updated_at = now() WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: OrganizeBrainCapture :one
UPDATE brain_capture SET status = $3, note_id = $4, organized_by = $5, organized_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'raw'
RETURNING *;

-- name: ReopenBrainCapture :one
UPDATE brain_capture SET status = 'raw', note_id = NULL, organized_by = NULL, organized_at = NULL, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'discarded'
RETURNING *;

-- name: PurgeWorkspaceBrainCaptures :exec
DELETE FROM brain_capture WHERE workspace_id = $1;

-- name: AttachAttachmentToCapture :exec
UPDATE attachment SET capture_id = $3 WHERE id = $1 AND workspace_id = $2;

-- name: AttachAttachmentToNote :exec
UPDATE attachment SET note_id = $3 WHERE id = $1 AND workspace_id = $2;

-- name: ListNoteAttachments :many
SELECT * FROM attachment WHERE workspace_id = $1 AND note_id = $2 ORDER BY created_at ASC;

-- name: GetWorkspaceNoteByID :one
SELECT * FROM workspace_note WHERE id = $1;

-- name: DeleteBrainCapture :execrows
DELETE FROM brain_capture WHERE id = $1 AND workspace_id = $2;
