DROP TRIGGER IF EXISTS trg_issue_completed_at ON issue;
DROP FUNCTION IF EXISTS issue_track_completed_at();
ALTER TABLE issue DROP COLUMN IF EXISTS completed_at;
