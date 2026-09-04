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
ALTER TABLE issue DROP COLUMN IF EXISTS reopen_count;
