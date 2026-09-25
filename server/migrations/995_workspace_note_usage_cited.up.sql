-- JEF-417: agents can now cite a Brain note inline in their final output or a
-- comment, as a Markdown link [title](mention://note/<uuid>). 'cited' is a
-- new usage kind: a citation the completion-time extraction pass found by
-- parsing the run's text, distinct from injected/retrieved/opened, which are
-- recorded live while the run is happening. 'citation' is its channel: the
-- post-hoc pass, not any of the live client paths the other channels name.
-- NOT VALID keeps this an instant catalog change; every existing row already
-- satisfies the wider lists, which only add a value.
SET LOCAL lock_timeout = '2s';
ALTER TABLE workspace_note_usage DROP CONSTRAINT IF EXISTS workspace_note_usage_kind_check;
ALTER TABLE workspace_note_usage ADD CONSTRAINT workspace_note_usage_kind_check
    CHECK (kind IN ('injected', 'retrieved', 'opened', 'viewed', 'cited'))
    NOT VALID;
ALTER TABLE workspace_note_usage DROP CONSTRAINT IF EXISTS workspace_note_usage_channel_check;
ALTER TABLE workspace_note_usage ADD CONSTRAINT workspace_note_usage_channel_check
    CHECK (channel IN ('daemon_brief', 'native_brief', 'native_tool', 'api', 'mcp', 'file_read', 'web', 'mobile', 'citation'))
    NOT VALID;
