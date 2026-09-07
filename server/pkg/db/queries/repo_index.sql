-- Shared semantic repo index (K47). See migration 711 for the table's contract:
-- lexical always, vector only when an embeddings endpoint is configured, and
-- content_hash is the whole FILE's sha256 because a file is re-chunked as a
-- whole.

-- name: DiffRepoIndexFiles :many
-- The (path, hash) pairs already indexed for one repo. The daemon subtracts
-- this from what it found on disk to decide which files to re-chunk.
SELECT DISTINCT file_path, content_hash
FROM repo_index_chunk
WHERE workspace_id = $1
  AND repo_identifier = $2;

-- name: DeleteRepoIndexFileChunks :exec
-- First half of the atomic per-file replace; InsertRepoIndexChunk is the
-- second. Both run in one transaction so a file is never half-indexed.
DELETE FROM repo_index_chunk
WHERE workspace_id = $1
  AND repo_identifier = $2
  AND file_path = $3;

-- name: InsertRepoIndexChunk :exec
-- embedding is text-cast rather than a typed parameter: NULL stays NULL (the
-- row is then lexical-only) and a present value arrives as pgvector's own
-- '[a,b,c]' literal, which Postgres parses. A wrong-width vector is rejected
-- here by the column, which is the intended failure — see migration 711.
INSERT INTO repo_index_chunk (
    workspace_id, repo_identifier, file_path, symbol,
    start_line, end_line, content, content_hash, embedding, embedding_model, indexed_commit
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, CAST(sqlc.narg('embedding')::text AS vector),
    sqlc.narg('embedding_model')::text, $9
);

-- name: PruneRepoIndexPaths :exec
-- Drops the chunks of files that no longer exist on the repo's default branch.
-- An empty present_paths would wipe the repo, so the caller must never send one
-- for a repo it failed to walk; service.PruneRepoIndex refuses that explicitly.
DELETE FROM repo_index_chunk
WHERE workspace_id = $1
  AND repo_identifier = $2
  AND file_path <> ALL(@present_paths::text[]);

-- name: StampRepoIndexCommit :exec
-- Records that a completed pass verified every remaining chunk of this repo at
-- one commit.
--
-- Without this, `stale` would be useless. An incremental pass only rewrites the
-- files that CHANGED, so every untouched file would keep the commit it was
-- first indexed at, and a repository where one file moved would report its
-- other five hundred as stale — a caveat on every hint, meaning nothing. The
-- pass did look at those files: it compared their hashes and found them
-- current, which is exactly the assertion "verified at this commit". Stale then
-- means what it should: this chunk was not part of the last completed pass.
UPDATE repo_index_chunk
SET indexed_commit = $3
WHERE workspace_id = $1
  AND repo_identifier = $2
  AND indexed_commit <> $3;

-- name: RepoIndexStats :one
-- Per-repo summary for the Settings block: how much is indexed, at which
-- commit, and when. The commit is the one carried by the most recently written
-- chunk, which is also the commit staleness is measured against.
SELECT
    COUNT(*)::bigint AS chunk_count,
    COUNT(DISTINCT file_path)::bigint AS file_count,
    COALESCE(MAX(updated_at), '-infinity'::timestamptz)::timestamptz AS last_indexed_at,
    COUNT(*) FILTER (
        WHERE embedding IS NOT NULL
          AND NOT COALESCE(embedding_model = sqlc.narg('embedding_model')::text, false)
    )::bigint AS unusable_embedding_count,
    COALESCE((
        SELECT newest.indexed_commit
        FROM repo_index_chunk newest
        WHERE newest.workspace_id = $1
          AND newest.repo_identifier = $2
        ORDER BY newest.updated_at DESC
        LIMIT 1
    ), '')::text AS last_indexed_commit
FROM repo_index_chunk
WHERE workspace_id = $1
  AND repo_identifier = $2;

-- name: QueryRepoIndex :many
-- Hybrid retrieval in one statement.
--
-- Stage 1 (`lexical`) is the prefilter and the only stage a deployment without
-- embeddings has: full-text rank over the chunk body, plus name matches on the
-- symbol and the file path, which are what a short "where is X handled" query
-- actually hits. It takes a wider slice than the caller asked for so stage 2
-- has something to reorder.
--
-- Stage 2 adds cosine proximity when BOTH a query embedding and the chunk's own
-- embedding exist. `embedding <=> NULL` is NULL, so COALESCE(..., 0) leaves a
-- lexical-only row at its lexical score rather than dropping it — that COALESCE
-- is the whole reason lexical-only deployments keep working.
--
-- `stale` compares the chunk's commit to the repo's newest indexed commit: a
-- run is told when a hint predates the current index pass.
WITH query AS (
    -- plainto_tsquery ANDs its terms, which is the wrong join for this query:
    -- the search text is a whole issue title plus the head of its description,
    -- and no single code chunk contains every word of an issue. Rewriting the
    -- conjunction into a disjunction keeps plainto_tsquery's stemming, stopword
    -- handling and escaping — the parts that are genuinely hard — while letting
    -- a chunk match on the words it does share. ts_rank then does the ordering:
    -- a chunk matching six of the terms outranks one matching two.
    SELECT replace(plainto_tsquery('english', @query::text)::text, '&', '|')::tsquery AS tsq
), newest AS (
    SELECT n.indexed_commit
    FROM repo_index_chunk n
    WHERE n.workspace_id = @workspace_id
      AND n.repo_identifier = @repo_identifier
    ORDER BY n.updated_at DESC
    LIMIT 1
), lexical AS (
    SELECT
        c.id, c.file_path, c.symbol, c.start_line, c.end_line, c.content, c.indexed_commit,
        c.embedding, c.embedding_model,
        ts_rank(c.tsv, (SELECT query.tsq FROM query)) AS lex_rank,
        (CASE WHEN c.symbol <> '' AND c.symbol ILIKE '%' || @query::text || '%' THEN 1 ELSE 0 END
         + CASE WHEN c.file_path ILIKE '%' || @query::text || '%' THEN 1 ELSE 0 END)::float8 AS name_hits
    FROM repo_index_chunk c
    WHERE c.workspace_id = @workspace_id
      AND c.repo_identifier = @repo_identifier
      AND (
        c.tsv @@ (SELECT query.tsq FROM query)
        OR (c.symbol <> '' AND c.symbol ILIKE '%' || @query::text || '%')
        OR c.file_path ILIKE '%' || @query::text || '%'
      )
    ORDER BY lex_rank DESC, name_hits DESC
    LIMIT @prefilter::int
)
SELECT
    lexical.file_path,
    lexical.symbol,
    lexical.start_line,
    lexical.end_line,
    lexical.content,
    lexical.indexed_commit,
    (lexical.indexed_commit <> COALESCE((SELECT newest.indexed_commit FROM newest), lexical.indexed_commit))::boolean AS stale,
    (lexical.lex_rank
        + lexical.name_hits * 0.25
        + CASE WHEN lexical.embedding_model = sqlc.narg('embedding_model')::text
               THEN COALESCE(1 - (lexical.embedding <=> CAST(sqlc.narg('query_embedding')::text AS vector)), 0)
               ELSE 0 END * 2
    )::float8 AS score
FROM lexical
ORDER BY score DESC, lexical.file_path ASC, lexical.start_line ASC
LIMIT @top_k::int;

-- name: PurgeRepoIndexChunksForRepo :exec
-- Turning the index off for a repo in Settings deletes its chunks: the toggle
-- is the workspace's answer about whether this code may be stored at all, so
-- "off" must not leave the body text sitting in the table.
DELETE FROM repo_index_chunk
WHERE workspace_id = $1
  AND repo_identifier = $2;

-- name: PurgeWorkspaceRepoIndexChunks :exec
DELETE FROM repo_index_chunk WHERE workspace_id = $1;
