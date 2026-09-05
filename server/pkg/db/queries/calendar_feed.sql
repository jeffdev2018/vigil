-- name: UpsertUserCalendarFeed :one
-- Saving a URL clears the last outcome: the next fetch decides, and keeping
-- the previous error would report a fixed feed as still broken.
INSERT INTO user_calendar_feed (workspace_id, user_id, url)
VALUES (sqlc.arg('workspace_id')::uuid, sqlc.arg('user_id')::uuid, sqlc.arg('url')::text)
ON CONFLICT (workspace_id, user_id) DO UPDATE
SET url = EXCLUDED.url,
    last_fetched_at = NULL,
    last_error = '',
    updated_at = now()
RETURNING *;

-- name: GetUserCalendarFeed :one
SELECT * FROM user_calendar_feed
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND user_id = sqlc.arg('user_id')::uuid;

-- name: DeleteUserCalendarFeed :execrows
DELETE FROM user_calendar_feed
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND user_id = sqlc.arg('user_id')::uuid;

-- name: RecordUserCalendarFeedFetch :exec
-- The outcome of the last fetch, so Settings can say why a feed is silent.
-- The URL is matched too: a fetch that started before the user changed the
-- URL must not stamp its result onto the new one.
UPDATE user_calendar_feed
SET last_fetched_at = now(),
    last_error = sqlc.arg('last_error')::text,
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND user_id = sqlc.arg('user_id')::uuid
  AND url = sqlc.arg('url')::text;
