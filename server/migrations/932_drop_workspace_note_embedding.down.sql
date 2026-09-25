-- Recreates the pre-JEF-412 table empty and without its indexes; the
-- rollback of 903/904 drops those indexes with IF EXISTS.
CREATE TABLE IF NOT EXISTS workspace_note_embedding (
    note_id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    embedding vector(1536) NOT NULL,
    embedding_model TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
