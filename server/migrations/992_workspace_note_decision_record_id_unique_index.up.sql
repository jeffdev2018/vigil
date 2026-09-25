-- One note per decision record: what makes the mirror (on record creation,
-- and the backfill in 993) idempotent via ON CONFLICT DO NOTHING / NOT
-- EXISTS instead of a second read-then-write race.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_note_decision_record_id_idx
    ON workspace_note (decision_record_id)
    WHERE decision_record_id IS NOT NULL;
