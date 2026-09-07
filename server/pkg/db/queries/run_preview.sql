-- name: UpsertRunPreview :one
-- The daemon reports its preview once it has probed the port, then again on
-- every state change. Upsert rather than insert: a retried report after a
-- network blip must not fail, and a run that restarts its script keeps the same
-- row so the share links pointing at the task stay meaningful.
INSERT INTO run_preview (
    workspace_id, task_id, runtime_id, port, scheme, status, health_path, error, last_reported_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, sqlc.narg(error), now())
ON CONFLICT (task_id) DO UPDATE SET
    runtime_id       = EXCLUDED.runtime_id,
    port             = EXCLUDED.port,
    scheme           = EXCLUDED.scheme,
    status           = EXCLUDED.status,
    health_path      = EXCLUDED.health_path,
    error            = EXCLUDED.error,
    last_reported_at = now(),
    stopped_at       = CASE WHEN EXCLUDED.status IN ('stopped', 'error') THEN now() ELSE NULL END
RETURNING *;

-- name: StopRunPreview :one
-- The run ended. The row survives so the UI can say "the preview is gone"
-- rather than silently dropping a link the reviewer still has open, and so the
-- public proxy can answer 410 instead of 404.
UPDATE run_preview
SET status = 'stopped', stopped_at = now(), last_reported_at = now()
WHERE task_id = $1
RETURNING *;

-- name: GetRunPreviewByTask :one
SELECT * FROM run_preview WHERE task_id = $1;

-- name: MarkStaleRunPreviewsForGoneDaemons :execrows
-- A daemon that disappeared leaves its previews claiming `ready` forever: the
-- process is gone with the machine, but nothing reported it. Staleness is read
-- off the same runtime heartbeat freshness the run sweeper uses, so a preview
-- and its run agree on when the daemon left.
UPDATE run_preview p
SET status = 'stale', last_reported_at = now()
FROM agent_runtime rt
WHERE p.runtime_id = rt.id
  AND p.status IN ('starting', 'ready')
  AND EXTRACT(EPOCH FROM (now() - rt.last_seen_at)) > sqlc.arg(stale_after_seconds)::float8;

-- name: CountLiveRunPreviewsForWorkspace :one
SELECT COUNT(*) FROM run_preview WHERE workspace_id = $1 AND status IN ('starting', 'ready');

-- name: PurgeWorkspaceRunPreviews :exec
DELETE FROM run_preview WHERE workspace_id = $1;

-- name: CreateTaskShareLink :one
INSERT INTO task_share_link (workspace_id, task_id, code, capabilities, created_by, expires_at)
VALUES ($1, $2, $3, $4, sqlc.narg(created_by), $5)
RETURNING *;

-- name: GetTaskShareLinkByCode :one
-- The public resolver's only lookup. Deliberately unscoped: the code IS the
-- credential, so requiring a workspace here would mean taking one from the
-- request, which the visitor controls.
SELECT * FROM task_share_link WHERE code = $1;

-- name: ListTaskShareLinks :many
SELECT * FROM task_share_link
WHERE task_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: RevokeTaskShareLink :one
UPDATE task_share_link
SET revoked_at = now()
WHERE id = $1 AND workspace_id = $2 AND revoked_at IS NULL
RETURNING *;

-- name: TouchTaskShareLink :exec
-- Best-effort usage accounting on the hot proxy path: a failed bump must never
-- cost the visitor their response.
UPDATE task_share_link
SET use_count = use_count + 1, last_used_at = now()
WHERE id = $1;

-- name: PurgeWorkspaceTaskShareLinks :exec
DELETE FROM task_share_link WHERE workspace_id = $1;
