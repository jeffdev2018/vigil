-- Audit log (K08). Append and read only; the purge below is the sole
-- delete path and only works after AllowAuditPurge on the same transaction.

-- name: CreateAuditLogEntry :one
INSERT INTO audit_log_entry (workspace_id, actor_type, actor_id, action, entity_type, entity_id, model, cost_usd_ticks, approver_type, approver_id, details)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListAuditLogEntries :many
-- Keyset pagination on (occurred_at, id) descending; the filters are
-- optional and combine.
SELECT * FROM audit_log_entry
WHERE workspace_id = $1
  AND (sqlc.narg('since')::timestamptz IS NULL OR occurred_at >= sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('until')::timestamptz IS NULL OR occurred_at < sqlc.narg('until')::timestamptz)
  AND (sqlc.narg('actor_type')::text IS NULL OR actor_type = sqlc.narg('actor_type')::text)
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('entity_id')::uuid IS NULL OR entity_id = sqlc.narg('entity_id')::uuid)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL OR (occurred_at, id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY occurred_at DESC, id DESC
LIMIT sqlc.arg('page_size')::int;

-- name: AllowAuditPurge :one
SELECT set_config('multica.audit_purge', 'on', true)::text;

-- name: PurgeWorkspaceAuditLog :exec
DELETE FROM audit_log_entry WHERE workspace_id = $1;

-- name: VerifyAuditChain :one
-- Recomputes every hash with the same function as the insert trigger and
-- checks each link; returns the first broken entry, if any.
WITH ordered AS (
    SELECT id, chain_seq, prev_hash, hash,
           audit_log_entry_hash(prev_hash, workspace_id, chain_seq, occurred_at, actor_type, actor_id, action,
                                entity_type, entity_id, model, cost_usd_ticks, approver_type, approver_id, details) AS expected,
           lag(hash) OVER (ORDER BY chain_seq) AS previous
    FROM audit_log_entry
    WHERE workspace_id = $1
), broken AS (
    SELECT id, chain_seq FROM ordered
    WHERE hash <> expected OR coalesce(prev_hash, '') <> coalesce(previous, '')
    ORDER BY chain_seq LIMIT 1
)
SELECT (SELECT count(*) FROM ordered)::bigint AS total,
       (SELECT id FROM broken) AS broken_id,
       coalesce((SELECT chain_seq FROM broken), 0)::bigint AS broken_seq,
       coalesce((SELECT hash FROM ordered ORDER BY chain_seq DESC LIMIT 1), '')::text AS head_hash;
