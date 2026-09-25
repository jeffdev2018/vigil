-- A separate transaction scans under SHARE UPDATE EXCLUSIVE, allowing normal
-- reads/writes. No ACCESS EXCLUSIVE operation belongs in this file.
SET LOCAL lock_timeout = '2s';
ALTER TABLE workspace_note_usage VALIDATE CONSTRAINT workspace_note_usage_kind_check;
ALTER TABLE workspace_note_usage VALIDATE CONSTRAINT workspace_note_usage_channel_check;
