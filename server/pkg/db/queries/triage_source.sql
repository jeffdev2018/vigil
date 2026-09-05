-- Triage source configuration and the counters the per-source policy needs.
-- Kept apart from triage.sql, which owns the queue items themselves.

-- name: UpdateTriageSourceSettings :one
-- Patch the per-source policy. Every field is optional: a caller sending only
-- `mode` must not reset the source's cap or retention to a default.
UPDATE triage_source
SET mode = COALESCE(sqlc.narg('mode'), mode),
    auto_accept = COALESCE(sqlc.narg('auto_accept')::jsonb, auto_accept),
    cap_per_hour = COALESCE(sqlc.narg('cap_per_hour')::int, cap_per_hour),
    expiry_days = COALESCE(sqlc.narg('expiry_days')::int, expiry_days),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: CountTriageItemsForSourceSince :one
-- Anti-flood counter behind triage_source.cap_per_hour. Collapsed repeats of
-- one title count once, because they are one queue row.
SELECT COUNT(*)::bigint AS n
FROM triage_item
WHERE workspace_id = $1
  AND source_id = $2
  AND first_seen_at >= sqlc.arg('since')::timestamptz;

-- name: SetTriageSourceToken :one
-- Create the workspace's email intake source, or rotate its token. The stored
-- value is a sha256 digest; the clear token exists only in the response that
-- mints it.
INSERT INTO triage_source (workspace_id, kind, ref_id, name, mode, token_hash, created_by_id)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('kind'),
    sqlc.arg('ref_id')::uuid,
    sqlc.arg('name'),
    sqlc.arg('mode'),
    sqlc.arg('token_hash'),
    sqlc.narg('created_by_id')::uuid
)
ON CONFLICT (workspace_id, kind, ref_id) DO UPDATE
SET token_hash = excluded.token_hash, updated_at = now()
RETURNING *;

-- name: GetTriageSourceByTokenHash :one
SELECT * FROM triage_source
WHERE token_hash = sqlc.arg('token_hash') AND token_hash <> '';

-- name: GetTriageSourceByRef :one
SELECT * FROM triage_source
WHERE workspace_id = $1 AND kind = $2 AND ref_id = $3;
