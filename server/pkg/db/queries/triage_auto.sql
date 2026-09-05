-- Triage auto-ML (K61): resolved queue items are the examples; a pending
-- item is classified by its nearest resolved neighbours (full text).

-- name: ListTriageNeighbors :many
SELECT id, state, title,
       ts_rank_cd(to_tsvector('english', title || ' ' || body_markdown || ' ' || payload::text), q)::float8 AS score
FROM triage_item
CROSS JOIN websearch_to_tsquery('english', sqlc.arg(query)::text) q
WHERE workspace_id = $1 AND shadow = false AND state IN ('accepted', 'dismissed') AND id <> sqlc.arg(exclude_id)::uuid
  AND to_tsvector('english', title || ' ' || body_markdown || ' ' || payload::text) @@ q
ORDER BY score DESC, resolved_at DESC
LIMIT 10;

-- name: CountTriageExamples :one
SELECT COUNT(*) FROM triage_item WHERE workspace_id = $1 AND shadow = false AND state IN ('accepted', 'dismissed');

-- name: AutoDismissPendingTriageItem :one
UPDATE triage_item
SET state = 'dismissed', resolution_reason = $3, resolved_at = now(), resolved_by_type = 'system', resolved_by_id = NULL,
    revision = revision + 1, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: ReopenDismissedTriageItem :one
UPDATE triage_item
SET state = 'pending', resolution_reason = NULL, resolved_at = NULL, resolved_by_type = NULL, resolved_by_id = NULL,
    revision = revision + 1, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'dismissed'
RETURNING *;

-- name: ListTriageItemsByIDs :many
SELECT * FROM triage_item WHERE workspace_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[]);
