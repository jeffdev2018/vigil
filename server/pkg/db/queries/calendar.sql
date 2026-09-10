-- Native calendar (OS plan, chantier 19).

-- name: CreateCalendarEvent :one
INSERT INTO calendar_event (id, workspace_id, title, description, starts_at, ends_at, all_day, timezone, location, issue_id, project_id, status, created_by_type, created_by_id, source, external_id, decision_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
RETURNING *;

-- name: GetCalendarEvent :one
SELECT * FROM calendar_event WHERE id = $1 AND workspace_id = $2;

-- name: UpdateCalendarEvent :one
UPDATE calendar_event
SET title = $3, description = $4, starts_at = $5, ends_at = $6, all_day = $7, timezone = $8, location = $9, issue_id = $10, project_id = $11, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SetCalendarEventStatus :one
UPDATE calendar_event SET status = $3, updated_at = now() WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: SetCalendarEventDecision :exec
UPDATE calendar_event SET decision_id = $2, updated_at = now() WHERE id = $1;

-- name: SetCalendarEventExternalID :exec
UPDATE calendar_event SET source = $2, external_id = $3, updated_at = now() WHERE id = $1;

-- name: GetCalendarEventByDecision :one
SELECT * FROM calendar_event WHERE decision_id = $1 LIMIT 1;

-- name: DeleteCalendarEvent :exec
DELETE FROM calendar_event WHERE id = $1 AND workspace_id = $2;

-- name: ListCalendarEventsInWindow :many
-- Every event overlapping [from, to), cancelled ones included when asked.
SELECT * FROM calendar_event
WHERE workspace_id = sqlc.arg('workspace_id')
  AND starts_at < sqlc.arg('until')::timestamptz AND ends_at > sqlc.arg('since')::timestamptz
  AND (sqlc.arg('include_cancelled')::boolean OR status <> 'cancelled')
ORDER BY starts_at ASC, id ASC
LIMIT 1000;

-- name: ListCalendarEventsForParticipantInWindow :many
SELECT e.* FROM calendar_event e
JOIN calendar_event_participant p ON p.event_id = e.id
WHERE e.workspace_id = sqlc.arg('workspace_id')
  AND p.participant_type = sqlc.arg('participant_type') AND p.participant_id = sqlc.arg('participant_id')
  AND e.starts_at < sqlc.arg('until')::timestamptz AND e.ends_at > sqlc.arg('since')::timestamptz
  AND e.status <> 'cancelled'
ORDER BY e.starts_at ASC, e.id ASC
LIMIT 1000;

-- name: ListCalendarEventsForIssue :many
SELECT * FROM calendar_event WHERE workspace_id = $1 AND issue_id = $2 AND status <> 'cancelled' ORDER BY starts_at ASC;

-- name: AddCalendarEventParticipant :one
INSERT INTO calendar_event_participant (id, workspace_id, event_id, participant_type, participant_id, response, required)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (event_id, participant_type, participant_id) DO UPDATE SET required = EXCLUDED.required
RETURNING *;

-- name: DeleteCalendarEventParticipants :exec
DELETE FROM calendar_event_participant WHERE event_id = $1;

-- name: ListCalendarEventParticipants :many
SELECT * FROM calendar_event_participant WHERE event_id = ANY(sqlc.arg('event_ids')::uuid[]) ORDER BY created_at ASC;

-- name: SetCalendarEventParticipantResponse :one
UPDATE calendar_event_participant SET response = $4
WHERE event_id = $1 AND participant_type = $2 AND participant_id = $3
RETURNING *;

-- name: ListCalendarEventsToRemind :many
-- Scheduled events starting within the window that nobody was reminded of.
SELECT * FROM calendar_event
WHERE status = 'scheduled' AND reminded_at IS NULL
  AND starts_at > sqlc.arg('now')::timestamptz AND starts_at <= sqlc.arg('until')::timestamptz
ORDER BY starts_at ASC
LIMIT 200;

-- name: MarkCalendarEventReminded :one
UPDATE calendar_event SET reminded_at = now() WHERE id = $1 AND reminded_at IS NULL RETURNING id;

-- name: UpsertCalendarFeedToken :one
INSERT INTO calendar_feed_token (id, workspace_id, user_id, token_hash)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, created_at = now()
RETURNING *;

-- name: GetCalendarFeedTokenByHash :one
SELECT * FROM calendar_feed_token WHERE token_hash = $1;

-- name: GetCalendarFeedTokenForUser :one
SELECT * FROM calendar_feed_token WHERE workspace_id = $1 AND user_id = $2;

-- name: DeleteCalendarFeedToken :exec
DELETE FROM calendar_feed_token WHERE workspace_id = $1 AND user_id = $2;

-- name: ListIssuesDueBetween :many
SELECT id, number, title, status, due_date, assignee_type, assignee_id, project_id FROM issue
WHERE workspace_id = sqlc.arg('workspace_id') AND due_date IS NOT NULL
  AND due_date >= sqlc.arg('since')::date AND due_date < sqlc.arg('until')::date
ORDER BY due_date ASC, number ASC
LIMIT 500;

-- name: ListCyclesOverlapping :many
SELECT id, name, start_date, end_date FROM cycle
WHERE workspace_id = sqlc.arg('workspace_id') AND start_date < sqlc.arg('until')::date AND end_date >= sqlc.arg('since')::date
ORDER BY start_date ASC
LIMIT 200;

-- name: ListMeetingsBetween :many
SELECT id, title, status, started_at, ended_at FROM meeting
WHERE workspace_id = sqlc.arg('workspace_id') AND started_at >= sqlc.arg('since')::timestamptz AND started_at < sqlc.arg('until')::timestamptz
ORDER BY started_at ASC
LIMIT 500;

-- name: PurgeWorkspaceCalendarEvents :exec
DELETE FROM calendar_event WHERE workspace_id = $1;

-- name: PurgeWorkspaceCalendarEventParticipants :exec
DELETE FROM calendar_event_participant WHERE workspace_id = $1;

-- name: PurgeWorkspaceCalendarFeedTokens :exec
DELETE FROM calendar_feed_token WHERE workspace_id = $1;
