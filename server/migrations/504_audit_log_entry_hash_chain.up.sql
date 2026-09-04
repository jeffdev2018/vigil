-- Audit log (K08): every entry carries the hash of its content and of the
-- previous entry of its workspace, so a removed or altered row breaks the
-- chain. One definition of the hash, used by the insert trigger and by the
-- verification query; the advisory lock makes the writer single-file per
-- workspace so two concurrent inserts cannot claim the same predecessor.
ALTER TABLE audit_log_entry
    ADD COLUMN IF NOT EXISTS chain_seq BIGINT,
    ADD COLUMN IF NOT EXISTS prev_hash TEXT,
    ADD COLUMN IF NOT EXISTS hash TEXT;

CREATE OR REPLACE FUNCTION audit_log_entry_hash(
    p_prev_hash TEXT, p_workspace_id UUID, p_chain_seq BIGINT, p_occurred_at TIMESTAMPTZ,
    p_actor_type TEXT, p_actor_id UUID, p_action TEXT, p_entity_type TEXT, p_entity_id UUID,
    p_model TEXT, p_cost BIGINT, p_approver_type TEXT, p_approver_id UUID, p_details JSONB
) RETURNS TEXT
LANGUAGE sql IMMUTABLE
AS $$
    SELECT encode(sha256(convert_to(concat_ws('|',
        coalesce(p_prev_hash, ''), p_workspace_id::text, p_chain_seq::text,
        to_char(p_occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US'),
        p_actor_type, coalesce(p_actor_id::text, ''), p_action, p_entity_type, coalesce(p_entity_id::text, ''),
        coalesce(p_model, ''), coalesce(p_cost::text, ''), coalesce(p_approver_type, ''), coalesce(p_approver_id::text, ''),
        coalesce(p_details::text, '{}')
    ), 'UTF8')), 'hex');
$$;

-- The immutability trigger must let the backfill pass, and nothing else.
CREATE OR REPLACE FUNCTION audit_log_entry_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('multica.audit_purge', true) = 'on' THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' AND current_setting('multica.audit_backfill', true) = 'on' THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'audit_log_entry is append-only (%)', TG_OP USING ERRCODE = 'raise_exception';
END;
$$;

-- Backfill: existing rows join the chain in (occurred_at, id) order.
DO $$
DECLARE
    r RECORD;
    v_prev TEXT;
    v_seq BIGINT;
    v_ws UUID := NULL;
BEGIN
    FOR r IN SELECT * FROM audit_log_entry WHERE hash IS NULL ORDER BY workspace_id, occurred_at, id LOOP
        IF v_ws IS DISTINCT FROM r.workspace_id THEN
            v_ws := r.workspace_id;
            SELECT hash, chain_seq INTO v_prev, v_seq FROM audit_log_entry
             WHERE workspace_id = v_ws AND hash IS NOT NULL ORDER BY chain_seq DESC LIMIT 1;
            IF v_seq IS NULL THEN v_seq := 0; v_prev := NULL; END IF;
        END IF;
        v_seq := v_seq + 1;
        v_prev := audit_log_entry_hash(v_prev, r.workspace_id, v_seq, r.occurred_at, r.actor_type, r.actor_id, r.action,
            r.entity_type, r.entity_id, r.model, r.cost_usd_ticks, r.approver_type, r.approver_id, r.details);
        -- The immutability trigger lets the backfill through on its own flag.
        PERFORM set_config('multica.audit_backfill', 'on', true);
        UPDATE audit_log_entry SET chain_seq = v_seq, prev_hash = lag_prev.h, hash = v_prev
          FROM (SELECT hash AS h FROM audit_log_entry WHERE workspace_id = v_ws AND chain_seq = v_seq - 1) AS lag_prev
         WHERE id = r.id;
        IF NOT FOUND THEN
            UPDATE audit_log_entry SET chain_seq = v_seq, prev_hash = NULL, hash = v_prev WHERE id = r.id;
        END IF;
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION audit_log_entry_chain()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    v_prev TEXT;
    v_seq BIGINT;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('audit_log_entry:' || NEW.workspace_id::text));
    SELECT hash, chain_seq INTO v_prev, v_seq FROM audit_log_entry
     WHERE workspace_id = NEW.workspace_id ORDER BY chain_seq DESC LIMIT 1;
    NEW.chain_seq := coalesce(v_seq, 0) + 1;
    NEW.prev_hash := v_prev;
    NEW.hash := audit_log_entry_hash(v_prev, NEW.workspace_id, NEW.chain_seq, NEW.occurred_at, NEW.actor_type, NEW.actor_id,
        NEW.action, NEW.entity_type, NEW.entity_id, NEW.model, NEW.cost_usd_ticks, NEW.approver_type, NEW.approver_id, NEW.details);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_audit_log_entry_chain ON audit_log_entry;
CREATE TRIGGER trg_audit_log_entry_chain
BEFORE INSERT ON audit_log_entry
FOR EACH ROW EXECUTE FUNCTION audit_log_entry_chain();

ALTER TABLE audit_log_entry
    ALTER COLUMN chain_seq SET NOT NULL,
    ALTER COLUMN hash SET NOT NULL;
