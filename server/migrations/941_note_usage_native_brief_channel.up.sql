-- JEF-414: the native in-server runtime now injects Brain notes into its own
-- brief, so a note's usage rows need a channel that says which brief it was.
-- 'daemon_brief' stays what it always meant: the notes written into a daemon
-- run's .multica/knowledge directory.
-- NOT VALID keeps this an instant catalog change; every existing row already
-- satisfies the new list, which only adds a value.
ALTER TABLE workspace_note_usage DROP CONSTRAINT IF EXISTS workspace_note_usage_channel_check;
ALTER TABLE workspace_note_usage ADD CONSTRAINT workspace_note_usage_channel_check
    CHECK (channel IN ('daemon_brief', 'native_brief', 'native_tool', 'api', 'mcp', 'file_read', 'web', 'mobile'))
    NOT VALID;
