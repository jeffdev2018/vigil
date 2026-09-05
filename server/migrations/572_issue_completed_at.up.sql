-- Cost per deliverable (K04): when an issue entered its done category. Kept
-- by a trigger so every status write (handler, batch, agent completion,
-- forge sync) records it; leaving done clears it. Existing done issues get
-- their last update time, the closest signal available.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
COMMENT ON COLUMN issue.completed_at IS 'When the issue last entered a done-category status; NULL while it is not done.';

CREATE OR REPLACE FUNCTION issue_track_completed_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    is_done BOOLEAN;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.status IS NOT DISTINCT FROM OLD.status THEN
        RETURN NEW;
    END IF;
    is_done := NEW.status = 'done' OR EXISTS (
        SELECT 1 FROM issue_status s
        WHERE s.workspace_id = NEW.workspace_id AND s.key = NEW.status AND s.category = 'done'
    );
    IF is_done THEN
        NEW.completed_at := now();
    ELSE
        NEW.completed_at := NULL;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_issue_completed_at ON issue;
CREATE TRIGGER trg_issue_completed_at
BEFORE INSERT OR UPDATE OF status ON issue
FOR EACH ROW EXECUTE FUNCTION issue_track_completed_at();

UPDATE issue i
SET completed_at = i.updated_at
WHERE i.completed_at IS NULL
  AND (i.status = 'done' OR EXISTS (
    SELECT 1 FROM issue_status s
    WHERE s.workspace_id = i.workspace_id AND s.key = i.status AND s.category = 'done'
  ));
