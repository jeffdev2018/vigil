-- Brain note usage (JEF-413). Every write is best-effort for its caller and
-- idempotent: the conflict targets repeat the partial unique indexes'
-- predicates, so a repeated use inserts nothing.
-- ponytail: raw rows, no retention; roll them into a daily aggregate when the
-- volume asks for it.

-- name: RecordRunNoteUsage :exec
-- One kind of use by one run over several notes. The note must live in the
-- agent's workspace, which is where workspace_id comes from, so no caller can
-- attribute a use across tenants. Duplicate ids in the input collapse.
INSERT INTO workspace_note_usage (
    id, workspace_id, note_id, note_revision, kind, channel, actor_type, actor_id, task_id
)
SELECT gen_random_uuid(), n.workspace_id, n.id, v.note_revision,
    sqlc.arg('kind')::text, sqlc.arg('channel')::text, 'agent', a.id, sqlc.arg('task_id')::uuid
FROM (
    SELECT DISTINCT ON (u.note_id) u.note_id, u.note_revision
    FROM (
        SELECT unnest(sqlc.arg('note_ids')::uuid[]) AS note_id,
               unnest(sqlc.arg('note_revisions')::bigint[]) AS note_revision
    ) u
) v
JOIN workspace_note n ON n.id = v.note_id
JOIN agent a ON a.id = sqlc.arg('agent_id')::uuid AND a.workspace_id = n.workspace_id
ON CONFLICT (task_id, note_id, kind) WHERE task_id IS NOT NULL DO NOTHING;

-- name: RecordMemberNoteView :exec
-- A member read the note; counted once per note, person and UTC day.
INSERT INTO workspace_note_usage (
    id, workspace_id, note_id, note_revision, kind, channel, actor_type, actor_id, task_id
)
VALUES (
    gen_random_uuid(), sqlc.arg('workspace_id')::uuid, sqlc.arg('note_id')::uuid,
    sqlc.arg('note_revision')::bigint, 'viewed', sqlc.arg('channel')::text, 'member',
    sqlc.arg('user_id')::uuid, NULL
)
ON CONFLICT (note_id, actor_id, kind, day) WHERE task_id IS NULL DO NOTHING;

-- name: GetWorkspaceNoteUsageSummary :one
SELECT
    count(*) FILTER (WHERE kind = 'injected')::bigint AS injected,
    count(*) FILTER (WHERE kind = 'retrieved')::bigint AS retrieved,
    count(*) FILTER (WHERE kind = 'opened')::bigint AS opened,
    count(*) FILTER (WHERE kind = 'viewed')::bigint AS viewed,
    count(*) FILTER (WHERE kind = 'cited')::bigint AS cited,
    count(DISTINCT task_id)::bigint AS runs_count,
    count(DISTINCT actor_id) FILTER (WHERE actor_type = 'member')::bigint AS viewers_count,
    max(created_at)::timestamptz AS last_used_at
FROM workspace_note_usage
WHERE note_id = sqlc.arg('note_id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: ListWorkspaceNoteUsageRuns :many
-- The note's most recent runs. is_chat is the same marker SetTaskMemoryContext
-- stamps: a chat session, or chat evidence surviving the session's deletion.
SELECT
    u.task_id::uuid AS task_id,
    (array_agg(u.actor_id))[1]::uuid AS agent_id,
    COALESCE(a.name, '')::text AS agent_name,
    t.issue_id,
    COALESCE(w.issue_prefix || '-' || i.number::text, '')::text AS issue_identifier,
    t.chat_session_id,
    COALESCE(t.chat_session_id IS NOT NULL OR t.trigger_evidence_kind = 'chat', false)::bool AS is_chat,
    array_agg(DISTINCT u.kind ORDER BY u.kind)::text[] AS kinds,
    min(u.created_at)::timestamptz AS first_at,
    max(u.created_at)::timestamptz AS last_at
FROM workspace_note_usage u
LEFT JOIN agent_task_queue t ON t.id = u.task_id
LEFT JOIN agent a ON a.id = u.actor_id
LEFT JOIN issue i ON i.id = t.issue_id
LEFT JOIN workspace w ON w.id = i.workspace_id
WHERE u.note_id = sqlc.arg('note_id')::uuid
    AND u.workspace_id = sqlc.arg('workspace_id')::uuid
    AND u.task_id IS NOT NULL
GROUP BY u.task_id, a.name, t.issue_id, w.issue_prefix, i.number, t.chat_session_id, t.trigger_evidence_kind
ORDER BY max(u.created_at) DESC
LIMIT sqlc.arg('row_limit')::int;

-- name: ListTaskNoteUsage :many
-- The notes one run used, first use first. Note deletion removes usage rows,
-- so the claim's own record (memory_context.workspace_notes) fills in the
-- injections whose row is gone: a note deleted since still appears, with
-- deleted = true, and an injection whose best-effort insert failed appears too.
WITH uses AS (
    SELECT u.note_id, u.note_revision, u.kind, u.channel, u.created_at
    FROM workspace_note_usage u
    WHERE u.task_id = sqlc.arg('task_id')::uuid AND u.workspace_id = sqlc.arg('workspace_id')::uuid
    UNION ALL
    SELECT (e.value->>'id')::uuid, (e.value->>'revision')::bigint, 'injected'::text, 'daemon_brief'::text, t.dispatched_at
    FROM agent_task_queue t
    CROSS JOIN LATERAL jsonb_array_elements(
        CASE WHEN jsonb_typeof(t.memory_context->'workspace_notes') = 'array'
            THEN t.memory_context->'workspace_notes' ELSE '[]'::jsonb END
    ) e
    WHERE t.id = sqlc.arg('task_id')::uuid
        AND e.value->>'id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND jsonb_typeof(e.value->'revision') = 'number'
        AND NOT EXISTS (
            SELECT 1 FROM workspace_note_usage x
            WHERE x.task_id = t.id AND x.kind = 'injected' AND x.note_id::text = e.value->>'id'
        )
)
SELECT
    uses.note_id::uuid AS note_id,
    COALESCE(n.title, '')::text AS title,
    (n.id IS NULL)::bool AS deleted,
    COALESCE(max(uses.note_revision), 0)::bigint AS note_revision,
    array_agg(DISTINCT uses.kind ORDER BY uses.kind)::text[] AS kinds,
    array_agg(DISTINCT uses.channel ORDER BY uses.channel)::text[] AS channels,
    min(uses.created_at)::timestamptz AS first_at
FROM uses
LEFT JOIN workspace_note n ON n.id = uses.note_id AND n.workspace_id = sqlc.arg('workspace_id')::uuid
GROUP BY uses.note_id, n.id, n.title
ORDER BY min(uses.created_at), uses.note_id;
