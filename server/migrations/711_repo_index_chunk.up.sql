-- Shared semantic repo index (K47). One row per code chunk of a repository the
-- workspace opted in to, produced by the daemon after a run (it holds the
-- checkout; the server never clones) and read back at claim time so a run is
-- oriented before its first manual exploration.
--
-- Hybrid by design, lexical first. `tsv` is always populated, so search works
-- on a deployment with no embeddings endpoint at all. `embedding` is filled
-- only when MULTICA_LLM_EMBEDDING_MODEL is configured; a NULL embedding must
-- never drop a row from the ranking, only leave it lexically ranked.
--
-- 1536 dimensions matches text-embedding-3-small, the default this feature is
-- sized for. A deployment pointing MULTICA_LLM_EMBEDDING_MODEL at a model of a
-- different width gets its vectors rejected by the column and stays lexical —
-- which is the correct failure: silently truncating or padding a vector would
-- produce a ranking that is confidently wrong.
--
-- content_hash is the sha256 of the WHOLE FILE the chunk came from, not of the
-- chunk. The unit of staleness is the file: a changed file is re-chunked as a
-- whole, so every chunk of one file carries the same hash and the incremental
-- diff is a single DISTINCT read.
--
-- No foreign keys by repository convention: workspace teardown deletes these
-- rows explicitly (purge step in handler/workspace.go), and disabling a repo
-- in Settings purges that repo's rows.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS repo_index_chunk (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    -- Canonical repo URL, the same string the daemon knows from RepoData.URL
    -- (a project's github_repo resource, or a workspace repo).
    repo_identifier TEXT NOT NULL,
    file_path       TEXT NOT NULL CHECK (file_path <> ''),
    -- Detected top-level symbol (func/class/type name); '' when the chunk is a
    -- plain fallback window.
    symbol          TEXT NOT NULL DEFAULT '',
    start_line      INT  NOT NULL DEFAULT 0,
    end_line        INT  NOT NULL DEFAULT 0,
    content         TEXT NOT NULL,
    content_hash    TEXT NOT NULL,
    tsv             TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
    embedding       vector(1536),
    -- Commit the chunk was produced from. A chunk whose commit differs from the
    -- repo's newest indexed commit is reported to the run as stale.
    indexed_commit  TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
