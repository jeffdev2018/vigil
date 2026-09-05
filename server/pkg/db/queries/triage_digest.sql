-- Queries for the daily "triage is stalling" digest. Read-only over
-- triage_item: the digest never changes the queue, it only reports on it.

-- name: ListWorkspacesWithStalePendingTriage :many
-- Every workspace whose queue holds real (non-shadow) pending items older
-- than the cutoff, with how many and when the oldest arrived. One statement
-- for the whole install: the job runs globally, and asking per workspace
-- would be one round trip per row of `workspace` to find the few that are
-- behind. Shadow items are excluded for the same reason
-- OldestRealPendingTriageAgeSeconds excludes them: nobody is waiting on them.
SELECT workspace_id,
       COUNT(*)::bigint AS pending_count,
       MIN(first_seen_at)::timestamptz AS oldest_first_seen_at
FROM triage_item
WHERE state = 'pending'
  AND shadow = false
  AND first_seen_at <= sqlc.arg('older_than')::timestamptz
GROUP BY workspace_id;

-- name: CountInboxItemsForDay :one
-- Dedup guard for a per-day digest: was this recipient already told today?
-- The day is the workspace's own calendar day, carried in details so a
-- workspace whose midnight is not UTC still gets exactly one digest per local
-- day (created_at could not answer that without re-deriving the timezone).
SELECT COUNT(*)::bigint FROM inbox_item
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND recipient_id = sqlc.arg('recipient_id')::uuid
  AND type = sqlc.arg('type')::text
  AND details->>'day' = sqlc.arg('day')::text;
