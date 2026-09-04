-- Morning briefing (K30).

-- name: ListWorkspacesForBriefing :many
SELECT id, settings FROM workspace ORDER BY created_at ASC;

-- name: ListIssuesCompletedBetween :many
SELECT * FROM issue
WHERE workspace_id = $1 AND completed_at >= $2 AND completed_at < $3
ORDER BY completed_at DESC
LIMIT 50;

-- name: ListIssuesInStatuses :many
SELECT * FROM issue
WHERE workspace_id = $1 AND status = ANY(sqlc.arg('statuses')::text[])
ORDER BY updated_at DESC
LIMIT 50;

-- name: ListPendingDecisionsForWorkspace :many
SELECT * FROM issue_decision
WHERE workspace_id = $1 AND response IS NULL
ORDER BY created_at DESC
LIMIT 200;

-- name: RecordMorningBriefingSent :one
-- The unique index is the idempotency guard: a second send for the date
-- matches no row.
INSERT INTO morning_briefing_sent (workspace_id, sent_for_date, channels_delivered, summary)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, sent_for_date) DO NOTHING
RETURNING *;

-- name: GetMorningBriefingSent :one
SELECT * FROM morning_briefing_sent WHERE workspace_id = $1 AND sent_for_date = $2;
