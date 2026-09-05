-- name: CreateMeeting :one
INSERT INTO meeting (workspace_id, created_by, title, app_name)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('created_by')::uuid,
    sqlc.arg('title')::text,
    sqlc.arg('app_name')::text
)
RETURNING *;

-- name: GetMeeting :one
SELECT * FROM meeting
WHERE id = $1 AND workspace_id = $2;

-- name: ListMeetings :many
-- action_count lets the list show how much a meeting produced without
-- loading its items; the detail endpoint carries the items themselves.
SELECT sqlc.embed(meeting),
       (SELECT COUNT(*) FROM triage_item ti
         WHERE ti.workspace_id = meeting.workspace_id
           AND ti.origin_type = 'meeting'
           AND ti.origin_id = meeting.id)::int AS action_count
FROM meeting
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg('page_limit')::int
OFFSET sqlc.arg('page_offset')::int;

-- name: AppendMeetingSegment :one
-- Appends one transcribed segment. Only a recording meeting accepts text;
-- the WHERE makes a late segment after finish a no-op the handler reports.
UPDATE meeting
SET transcript = CASE WHEN transcript = '' THEN sqlc.arg('text')::text
                      ELSE transcript || E'\n' || sqlc.arg('text')::text END,
    segment_count = segment_count + 1,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND status = 'recording'
RETURNING *;

-- name: StartMeetingSummary :one
-- recording -> summarizing. Zero rows means the meeting is not in the
-- recording state (already finishing, done, or failed).
UPDATE meeting
SET status = 'summarizing', ended_at = COALESCE(ended_at, now()), updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND status = 'recording'
RETURNING *;

-- name: CompleteMeeting :one
UPDATE meeting
SET status = 'done', summary_md = sqlc.arg('summary_md')::text, updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: FailMeeting :exec
UPDATE meeting
SET status = 'failed', updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: ListTriageItemsByOrigin :many
-- Action items extracted from one meeting, in capture order.
SELECT * FROM triage_item
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND origin_type = sqlc.arg('origin_type')::text
  AND origin_id = sqlc.arg('origin_id')::uuid
ORDER BY first_seen_at ASC, id ASC;

-- name: DeleteMeeting :execrows
-- Action items already captured into triage are deliberately NOT removed:
-- they are work items in their own right and the meeting is only where they
-- came from. Zero rows means the meeting was already gone.
DELETE FROM meeting
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: RenameMeeting :one
UPDATE meeting
SET title = sqlc.arg('title')::text, updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: RestartMeetingSummary :one
-- Re-runs the summary for a meeting that already stopped recording: a `done`
-- one whose summary was never written (no LLM at the time), a `failed` one, or
-- a `summarizing` one whose finish request died mid-flight. A finish still in
-- flight is protected by stale_after_seconds. Zero rows means "not eligible".
UPDATE meeting
SET status = 'summarizing', ended_at = COALESCE(ended_at, now()), updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND (
    status IN ('done', 'failed')
    OR (status = 'summarizing'
        AND updated_at < now() - make_interval(secs => sqlc.arg('stale_after_seconds')::int))
  )
RETURNING *;

-- name: SetMeetingTranscript :one
-- Rewrites the transcript of a finished meeting, for a manual segment edit.
-- `previous` is a compare-and-set: two people editing different paragraphs of
-- the same transcript would otherwise silently overwrite each other, since the
-- transcript is one column. Zero rows means the meeting is no longer `done` or
-- someone else saved first; the handler asks the client to reload.
UPDATE meeting
SET transcript = sqlc.arg('transcript')::text,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND status = 'done'
  AND transcript = sqlc.arg('previous')::text
RETURNING *;
