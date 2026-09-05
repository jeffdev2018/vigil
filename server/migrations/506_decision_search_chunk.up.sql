-- Why search (K55): one searchable chunk per comment, decision record or
-- agent text message, kept in step with its source. Full-text search on a
-- generated tsvector; an embedding column can join later without moving
-- the rows.
CREATE TABLE decision_search_chunk (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    source_type  TEXT NOT NULL CHECK (source_type IN ('comment', 'task_message', 'decision_record')),
    source_id    UUID NOT NULL,
    issue_id     UUID,
    content      TEXT NOT NULL,
    tsv          TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
