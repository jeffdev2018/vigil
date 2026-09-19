-- Brain capture (OS plan, vague B): "capture first, organize later". A raw
-- capture is text, a link, an image, an audio memo or a file parked in the
-- workspace's Brain inbox; a person (helped by a suggestion) turns it into a
-- note, merges it into one, or discards it. Also gives notes what ranked
-- search needs: a stored tsvector and an optional embedding.
CREATE TABLE brain_capture (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('text', 'link', 'image', 'audio', 'file', 'todo')),
    content TEXT NOT NULL DEFAULT '' CHECK (length(content) <= 20000),
    url TEXT NOT NULL DEFAULT '',
    title_hint TEXT NOT NULL DEFAULT '' CHECK (length(title_hint) <= 200),
    attachment_id UUID,
    origin TEXT NOT NULL DEFAULT 'web' CHECK (origin IN ('web', 'desktop', 'mobile', 'cli', 'mcp', 'channel', 'agent', 'api')),
    status TEXT NOT NULL DEFAULT 'raw' CHECK (status IN ('raw', 'organized', 'discarded')),
    transcription_status TEXT NOT NULL DEFAULT 'none' CHECK (transcription_status IN ('none', 'pending', 'done', 'failed')),
    suggestion JSONB,
    note_id UUID,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent', 'system')),
    created_by_id UUID,
    source_task_id UUID,
    organized_by UUID,
    organized_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE EXTENSION IF NOT EXISTS vector;

-- Embeddings live beside the note, not on it: every note read is a
-- `SELECT *`, and a vector column would ride into each of them. One row per
-- note, replaced when the content changes; a missing or mismatched model
-- means the note ranks lexically only.
CREATE TABLE workspace_note_embedding (
    note_id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    embedding vector(1536) NOT NULL,
    embedding_model TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A note organized from a capture keeps that provenance.
ALTER TABLE workspace_note DROP CONSTRAINT IF EXISTS workspace_note_source_check;
ALTER TABLE workspace_note ADD CONSTRAINT workspace_note_source_check CHECK (source IN ('manual', 'agent', 'curation', 'capture'));

ALTER TABLE attachment ADD COLUMN IF NOT EXISTS capture_id UUID;
ALTER TABLE attachment ADD COLUMN IF NOT EXISTS note_id UUID;
