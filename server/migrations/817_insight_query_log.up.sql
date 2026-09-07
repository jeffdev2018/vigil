-- F27 (JEF-25): one row per question asked in plain language, with the
-- document it compiled to and how it ended.
--
-- This table is the evidence for a decision that has NOT been made: whether
-- the DSL is wide enough. `invalid` (the model produced a document the
-- compiler rejected) is deliberately distinct from `refused` (no assist layer,
-- or the model gave up), because only the first one argues for widening the
-- vocabulary. Widening it before this table has data would be guessing.
--
-- `compiled` is the DSL document, not SQL, and is NULL when nothing compiled.
-- No FOREIGN KEY, per the repository rule.
CREATE TABLE IF NOT EXISTS insight_query_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    user_id UUID,
    question TEXT NOT NULL DEFAULT '',
    compiled JSONB,
    outcome TEXT NOT NULL CHECK (outcome IN ('ok', 'invalid', 'timeout', 'refused')),
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE insight_query_log IS
    'F27: translation-quality evidence. One row per plain-language question; outcome distinguishes a rejected document from an absent model.';
