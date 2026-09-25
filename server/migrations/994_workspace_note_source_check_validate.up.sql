-- A separate transaction scans under SHARE UPDATE EXCLUSIVE, allowing normal
-- reads/writes. No ACCESS EXCLUSIVE operation belongs in this file.
SET LOCAL lock_timeout = '2s';
ALTER TABLE workspace_note VALIDATE CONSTRAINT workspace_note_source_check;
