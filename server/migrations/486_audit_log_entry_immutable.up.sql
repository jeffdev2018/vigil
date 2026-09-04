-- Audit log (K08): rows are never updated; they are deleted only by the
-- workspace purge, which sets multica.audit_purge on its transaction first.
CREATE OR REPLACE FUNCTION audit_log_entry_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('multica.audit_purge', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'audit_log_entry is append-only (%)', TG_OP USING ERRCODE = 'raise_exception';
END;
$$;

DROP TRIGGER IF EXISTS trg_audit_log_entry_immutable ON audit_log_entry;
CREATE TRIGGER trg_audit_log_entry_immutable
BEFORE UPDATE OR DELETE ON audit_log_entry
FOR EACH ROW EXECUTE FUNCTION audit_log_entry_immutable();
