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

-- name: CountPendingTriageItemsBySource :many
-- What a human still has to look at, per source: real pending items that are
-- due. Distinct from the 24h volume counter, which also counts what has since
-- been resolved and what was dropped.
SELECT source_id, COUNT(*)::bigint AS n
FROM triage_item
WHERE workspace_id = $1 AND state = 'pending' AND shadow = false
  AND (snoozed_until IS NULL OR snoozed_until <= now())
GROUP BY source_id;

-- name: OldestRealPendingTriageAgeSeconds :one
SELECT COALESCE(EXTRACT(EPOCH FROM (now() - min(first_seen_at)))::bigint, 0)::bigint AS age_seconds
FROM triage_item
WHERE workspace_id = $1 AND state = 'pending' AND shadow = false
  AND (snoozed_until IS NULL OR snoozed_until <= now());

-- name: ExpirePendingTriageItems :execrows
-- Retention sweep, all workspaces, one bounded batch. An item nobody resolved
-- inside its retention window leaves the queue as 'expired' instead of being
-- deleted: resolved items are the auto-classifier's training examples (K61),
-- and a deleted row teaches it nothing. Expiring also frees the item's slot in
-- uq_triage_item_pending_title, so the same title can be captured again.
UPDATE triage_item
SET state = 'expired',
    resolution_reason = 'retention: expired unresolved',
    resolved_at = now(),
    resolved_by_type = 'system',
    revision = revision + 1,
    updated_at = now()
WHERE id IN (
    SELECT id FROM triage_item
    WHERE state = 'pending' AND expires_at IS NOT NULL AND expires_at <= now()
    ORDER BY expires_at
    LIMIT sqlc.arg('page_limit')::int
);

-- name: ListTriageItems :many
SELECT * FROM triage_item
WHERE workspace_id = $1
  AND shadow = false
  AND state = sqlc.arg('state')
  AND (
      sqlc.arg('include_snoozed')::boolean
      OR snoozed_until IS NULL
      OR snoozed_until <= now()
  )
  AND (
      sqlc.narg('cursor_time')::timestamptz IS NULL
      OR (first_seen_at, id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY first_seen_at DESC, id DESC
LIMIT sqlc.arg('page_limit')::int;

-- name: LockTriageItemForResolution :one
SELECT * FROM triage_item
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: AcceptPendingTriageItem :one
-- The acceptor is not always a human: an auto-accept resolves the item as
-- 'system' with no resolver id (the issue still needs a creator, which is a
-- separate identity), and records why it was accepted the way the auto-dismiss
-- path already does.
UPDATE triage_item
SET state = 'accepted',
    issue_id = sqlc.arg('issue_id')::uuid,
    resolution_reason = sqlc.narg('resolution_reason'),
    resolved_at = now(),
    resolved_by_type = sqlc.arg('resolved_by_type'),
    resolved_by_id = sqlc.narg('resolved_by')::uuid,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: DismissPendingTriageItem :one
UPDATE triage_item
SET state = 'dismissed',
    resolution_reason = sqlc.narg('resolution_reason'),
    resolved_at = now(),
    resolved_by_type = 'member',
    resolved_by_id = sqlc.arg('resolved_by')::uuid,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: MergePendingTriageItem :one
UPDATE triage_item
SET state = 'merged',
    duplicate_of_issue_id = sqlc.arg('duplicate_of_issue_id')::uuid,
    resolved_at = now(),
    resolved_by_type = 'member',
    resolved_by_id = sqlc.arg('resolved_by')::uuid,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: UpdateTriageSourceMode :one
UPDATE triage_source
SET mode = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SnoozePendingTriageItem :one
-- Park a pending item until a chosen time. The state stays 'pending': a
-- snooze hides an item, it never resolves one.
UPDATE triage_item
SET snoozed_until = sqlc.arg('snoozed_until')::timestamptz,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: CountSnoozedTriageItems :one
-- Visible pending items still parked in the future — the "Snoozed" tab count.
SELECT COUNT(*)::bigint AS n
FROM triage_item
WHERE workspace_id = $1
  AND state = 'pending'
  AND shadow = false
  AND snoozed_until IS NOT NULL
  AND snoozed_until > now();

-- name: WakeDueSnoozedTriageItems :many
-- Sweep, all workspaces, one bounded batch: a snooze whose time has come is
-- cleared so the item is announced again. The listing already stopped hiding
-- it at due time, so this only owns the re-announcement.
UPDATE triage_item
SET snoozed_until = NULL,
    revision = revision + 1,
    updated_at = now()
WHERE id IN (
    SELECT id FROM triage_item
    WHERE state = 'pending' AND snoozed_until IS NOT NULL AND snoozed_until <= now()
    ORDER BY snoozed_until
    LIMIT sqlc.arg('page_limit')::int
)
RETURNING id, workspace_id;

-- name: SetTriageItemVerdict :one
-- An agent's suggested verdict. It never changes state — humans decide — so
-- it applies to a pending item only, and each write bumps verdict_revision.
UPDATE triage_item
SET verdict = sqlc.arg('verdict')::jsonb,
    verdict_agent_id = sqlc.arg('verdict_agent_id')::uuid,
    verdict_at = now(),
    verdict_revision = verdict_revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;
