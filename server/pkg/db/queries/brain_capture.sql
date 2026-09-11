-- Brain capture (OS plan, vague B): the raw inbox, its organization into
-- notes, ranked note search and note embeddings.

-- name: CreateBrainCapture :one
INSERT INTO brain_capture (id, workspace_id, kind, content, url, title_hint, attachment_id, origin, transcription_status, created_by_type, created_by_id, source_task_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetBrainCapture :one
SELECT * FROM brain_capture WHERE id = $1 AND workspace_id = $2;

-- name: ListBrainCaptures :many
SELECT * FROM brain_capture
WHERE workspace_id = $1 AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: CountRawBrainCaptures :one
SELECT count(*) FROM brain_capture WHERE workspace_id = $1 AND status = 'raw';

-- name: SetBrainCaptureSuggestion :one
UPDATE brain_capture SET suggestion = $3, updated_at = now() WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: SetBrainCaptureTranscript :one
UPDATE brain_capture SET content = $3, transcription_status = $4, updated_at = now() WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: OrganizeBrainCapture :one
UPDATE brain_capture SET status = $3, note_id = $4, organized_by = $5, organized_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'raw'
RETURNING *;

-- name: ReopenBrainCapture :one
UPDATE brain_capture SET status = 'raw', note_id = NULL, organized_by = NULL, organized_at = NULL, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'discarded'
RETURNING *;

-- name: PurgeWorkspaceBrainCaptures :exec
DELETE FROM brain_capture WHERE workspace_id = $1;

-- name: AttachAttachmentToCapture :exec
UPDATE attachment SET capture_id = $3 WHERE id = $1 AND workspace_id = $2;

-- name: AttachAttachmentToNote :exec
UPDATE attachment SET note_id = $3 WHERE id = $1 AND workspace_id = $2;

-- name: ListNoteAttachments :many
SELECT * FROM attachment WHERE workspace_id = $1 AND note_id = $2 ORDER BY created_at ASC;

-- name: SearchWorkspaceNotes :many
-- Ranked note search: lexical rank over the same expression the GIN index
-- builds (websearch syntax, 'simple' config for the polyglot corpus), fused
-- by reciprocal rank with the vector rank when a query embedding is given
-- and the stored vector came from the same model. A note without an
-- embedding still ranks lexically. The vector leg is a nearest-neighbour
-- list: it always has neighbours, however unrelated the query, and RRF keeps
-- only their rank. So a note only the vector finds surfaces solely when the
-- caller asks for neighbours (vector_only_hits: capture merge candidates,
-- which a model then judges); a user-facing search needs a lexical match.
-- Snippets come from ts_headline over the content.
WITH q AS (
    SELECT websearch_to_tsquery('simple', sqlc.arg('query')::text) AS tsq
), lexical AS (
    SELECT n.id,
           row_number() OVER (ORDER BY ts_rank_cd(to_tsvector('simple', n.title || ' ' || n.content), (SELECT tsq FROM q)) DESC, n.pinned DESC, n.updated_at DESC) AS lex_rank
    FROM workspace_note n
    WHERE n.workspace_id = sqlc.arg('workspace_id')
      AND (sqlc.arg('include_archived')::bool OR n.archived_at IS NULL)
      AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY(n.tags))
      AND to_tsvector('simple', n.title || ' ' || n.content) @@ (SELECT tsq FROM q)
    LIMIT sqlc.arg('prefilter')::int
), vec AS (
    SELECT e.note_id AS id,
           row_number() OVER (ORDER BY e.embedding <=> CAST(sqlc.narg('query_embedding')::text AS vector)) AS vec_rank
    FROM workspace_note_embedding e
    JOIN workspace_note n ON n.id = e.note_id
    WHERE sqlc.narg('query_embedding')::text IS NOT NULL
      AND e.workspace_id = sqlc.arg('workspace_id')
      AND e.embedding_model = sqlc.narg('embedding_model')::text
      AND (sqlc.arg('include_archived')::bool OR n.archived_at IS NULL)
      AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY(n.tags))
    ORDER BY e.embedding <=> CAST(sqlc.narg('query_embedding')::text AS vector)
    LIMIT sqlc.arg('prefilter')::int
), fused AS (
    SELECT COALESCE(l.id, v.id) AS id,
           (COALESCE(1.0 / (60 + l.lex_rank), 0) + COALESCE(1.0 / (60 + v.vec_rank), 0))::float8 AS score,
           l.lex_rank, v.vec_rank
    FROM lexical l FULL OUTER JOIN vec v ON v.id = l.id
    -- ponytail: no similarity floor, a fixed one cannot be calibrated across
    -- embedding models; semantic-only recall for search returns with JEF-412.
    WHERE l.id IS NOT NULL OR sqlc.arg('vector_only_hits')::bool
)
SELECT n.*, f.score, f.lex_rank, f.vec_rank,
       ts_headline('simple', n.content, (SELECT tsq FROM q), 'MaxWords=40, MinWords=15, MaxFragments=2, FragmentDelimiter=" … ", StartSel=<mark>, StopSel=</mark>') AS snippet
FROM fused f JOIN workspace_note n ON n.id = f.id
ORDER BY f.score DESC, n.pinned DESC, n.updated_at DESC
LIMIT sqlc.arg('top_k')::int;

-- name: UpsertWorkspaceNoteEmbedding :exec
INSERT INTO workspace_note_embedding (note_id, workspace_id, embedding, embedding_model, content_hash)
VALUES ($1, $2, CAST(sqlc.arg('embedding')::text AS vector), $3, $4)
ON CONFLICT (note_id) DO UPDATE SET embedding = EXCLUDED.embedding, embedding_model = EXCLUDED.embedding_model, content_hash = EXCLUDED.content_hash, updated_at = now();

-- name: DeleteWorkspaceNoteEmbedding :exec
DELETE FROM workspace_note_embedding WHERE note_id = $1;

-- name: PurgeWorkspaceNoteEmbeddings :exec
DELETE FROM workspace_note_embedding WHERE workspace_id = $1;

-- name: ListWorkspaceNotesNeedingEmbedding :many
-- Live notes whose stored vector is missing, from another model, or older
-- than their content.
SELECT n.id, n.workspace_id, n.title, n.content
FROM workspace_note n
LEFT JOIN workspace_note_embedding e ON e.note_id = n.id
WHERE n.archived_at IS NULL
  AND (e.note_id IS NULL OR e.embedding_model <> sqlc.arg('embedding_model')::text OR e.content_hash <> md5(n.title || E'\n' || n.content))
ORDER BY n.updated_at DESC
LIMIT $1;

-- name: GetWorkspaceNoteByID :one
SELECT * FROM workspace_note WHERE id = $1;

-- name: DeleteBrainCapture :execrows
DELETE FROM brain_capture WHERE id = $1 AND workspace_id = $2;
