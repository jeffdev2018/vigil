-- JEF-413: which runs and which people used a Brain note. One row per use:
-- a note injected into a daemon brief, retrieved by a search or opened by a
-- run, or viewed by a member. Rows are raw events, deduplicated by the
-- partial unique indexes 937/938: a run counts a note once per kind, a person
-- once per note, kind and UTC day. Nothing is aggregated or expired yet.
--
-- No foreign keys and no cascades, per the repository rule: note deletion and
-- workspace teardown delete these rows in the same statement as the notes.
-- The primary key is attached by 935/936 through a CONCURRENTLY-built index.
CREATE TABLE IF NOT EXISTS workspace_note_usage (
    id            UUID NOT NULL,
    workspace_id  UUID NOT NULL,
    note_id       UUID NOT NULL,
    note_revision BIGINT,
    kind          TEXT NOT NULL CHECK (kind IN ('injected', 'retrieved', 'opened', 'viewed')),
    channel       TEXT NOT NULL CHECK (channel IN ('daemon_brief', 'native_tool', 'api', 'mcp', 'file_read', 'web', 'mobile')),
    actor_type    TEXT NOT NULL CHECK (actor_type IN ('agent', 'member')),
    actor_id      UUID,
    task_id       UUID,
    day           DATE NOT NULL DEFAULT (now() AT TIME ZONE 'UTC')::date,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((actor_type = 'agent') = (task_id IS NOT NULL))
);

COMMENT ON TABLE workspace_note_usage IS
    'JEF-413: one use of a Brain note — injected/retrieved/opened by a run (task_id set) or viewed by a member (task_id NULL, counted once a day).';
COMMENT ON COLUMN workspace_note_usage.actor_id IS
    'agent id when actor_type = ''agent'', user id when actor_type = ''member''.';
