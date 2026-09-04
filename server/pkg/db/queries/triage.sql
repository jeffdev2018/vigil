-- name: UpsertTriageSource :one
INSERT INTO triage_source (id, workspace_id, kind, ref_id, name, mode, created_by_id)
VALUES (
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
    $1, $2, $3, $4, 'direct',
    sqlc.narg('created_by_id')::uuid
)
ON CONFLICT (workspace_id, kind, ref_id) DO UPDATE
SET name = excluded.name, updated_at = now()
RETURNING *;

-- name: GetTriageSource :one
SELECT * FROM triage_source
WHERE id = $1 AND workspace_id = $2;

-- name: ListTriageSources :many
SELECT * FROM triage_source
WHERE workspace_id = $1
ORDER BY created_at;

-- name: UpsertTriageItem :one
-- Insert one inbound item, or fold it into the source's existing pending item
-- for the same normalized title (collapse_count + 1). The conflict target is
-- uq_triage_item_pending_title; dropped/resolved rows never conflict, so a
-- repeat after resolution starts a fresh item.
INSERT INTO triage_item (
    id, workspace_id, source_id, origin_type, origin_id,
    dedupe_key, content_digest, title, normalized_title, body_markdown, payload,
    state, drop_reason, shadow, issue_id, expires_at
)
VALUES (
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('source_id')::uuid,
    sqlc.arg('origin_type'),
    sqlc.narg('origin_id')::uuid,
    sqlc.narg('dedupe_key'),
    sqlc.narg('content_digest'),
    sqlc.narg('title'),
    sqlc.narg('normalized_title'),
    sqlc.narg('body_markdown'),
    sqlc.narg('payload')::jsonb,
    sqlc.arg('state'),
    sqlc.narg('drop_reason'),
    sqlc.arg('shadow'),
    sqlc.narg('issue_id')::uuid,
    sqlc.narg('expires_at')::timestamptz
)
ON CONFLICT (workspace_id, source_id, normalized_title) WHERE state = 'pending'
DO UPDATE SET collapse_count = triage_item.collapse_count + 1, updated_at = now()
RETURNING *;

-- name: CountTriageItemsByState :many
SELECT state, shadow, COUNT(*)::bigint AS n
FROM triage_item
WHERE workspace_id = $1
GROUP BY state, shadow;

-- name: CountRecentTriageItemsBySource :many
SELECT source_id, state, COUNT(*)::bigint AS n
FROM triage_item
WHERE workspace_id = $1 AND first_seen_at >= now() - INTERVAL '24 hours'
GROUP BY source_id, state;

-- name: OldestRealPendingTriageAgeSeconds :one
SELECT COALESCE(EXTRACT(EPOCH FROM (now() - min(first_seen_at)))::bigint, 0)::bigint AS age_seconds
FROM triage_item
WHERE workspace_id = $1 AND state = 'pending' AND shadow = false;

-- name: DeleteExpiredTriageItems :execrows
DELETE FROM triage_item
WHERE workspace_id = $1 AND expires_at IS NOT NULL AND expires_at <= now();
