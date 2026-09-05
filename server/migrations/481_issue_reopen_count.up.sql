-- Scorecards (K25): how many times an issue left its done category. The
-- completed_at trigger (migration 473) already sees every status write;
-- it now counts the reopenings too.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS reopen_count INTEGER NOT NULL DEFAULT 0;

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
        IF TG_OP = 'UPDATE' AND OLD.completed_at IS NOT NULL THEN
            NEW.reopen_count := COALESCE(OLD.reopen_count, 0) + 1;
        END IF;
        NEW.completed_at := NULL;
    END IF;
    RETURN NEW;
END;
$$;
