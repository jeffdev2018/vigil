DELETE FROM workspace_note_usage WHERE channel = 'native_brief';
ALTER TABLE workspace_note_usage DROP CONSTRAINT IF EXISTS workspace_note_usage_channel_check;
ALTER TABLE workspace_note_usage ADD CONSTRAINT workspace_note_usage_channel_check
    CHECK (channel IN ('daemon_brief', 'native_tool', 'api', 'mcp', 'file_read', 'web', 'mobile'))
    NOT VALID;
