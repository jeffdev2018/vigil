-- Points a note mirrored from a decision record (source = 'decision') back
-- at the record it was mirrored from. No FK: resolved and cleaned up in
-- application code, per this repo's no-FK rule. Nullable, constant default
-- is a fast catalog-only change.
ALTER TABLE workspace_note ADD COLUMN decision_record_id UUID;

COMMENT ON COLUMN workspace_note.decision_record_id IS
    'Set on a note mirrored from a decision_record row; unique so the mirror is idempotent.';
