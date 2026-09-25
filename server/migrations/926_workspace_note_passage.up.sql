-- JEF-412: Brain search indexes a note as passages (its Markdown sections,
-- long ones windowed) instead of one row per note. Each passage carries the
-- note title, its heading path and its body twice: as written (shown back
-- to people) and folded by the server (accents removed, CJK split into
-- bigrams) for the 'simple' text search configuration, since the database
-- has neither unaccent nor pg_bigm. An optional per-passage vector replaces
-- workspace_note_embedding.
--
-- No foreign keys and no cascades, per the repository rule: note deletion
-- and workspace teardown delete passages in the same statement, and the
-- embedding backfill sweeps orphans. The primary key is attached by 927/928
-- through a CONCURRENTLY-built index.
CREATE TABLE IF NOT EXISTS workspace_note_passage (
    note_id         UUID NOT NULL,
    workspace_id    UUID NOT NULL,
    ordinal         INT NOT NULL CHECK (ordinal >= 1),
    heading         TEXT NOT NULL DEFAULT '',
    body            TEXT NOT NULL,
    search_title    TEXT NOT NULL,
    search_heading  TEXT NOT NULL,
    search_body     TEXT NOT NULL,
    tsv             TSVECTOR GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', search_title), 'A')
        || setweight(to_tsvector('simple', search_heading), 'B')
        || to_tsvector('simple', search_body)
    ) STORED,
    note_revision   BIGINT NOT NULL,
    chunker_version INT NOT NULL,
    content_hash    TEXT NOT NULL,
    embedding       vector(1536),
    embedding_model TEXT,
    embedded_at     TIMESTAMPTZ,
    indexed_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE workspace_note_passage IS
    'JEF-412: one searchable passage of a Brain note. Rebuilt from the note when its revision or the chunker version changes; embedding kept while content_hash is unchanged.';
COMMENT ON COLUMN workspace_note_passage.content_hash IS
    'md5 of title, heading and body: the identity of what embedding was computed from.';
