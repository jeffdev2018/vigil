-- Brain search (JEF-412): notes indexed as passages, and the one ranked
-- search the REST endpoint, the palette, the Brain page, the CLI, MCP and the
-- native agent tools share. Text arrives folded by the server
-- (service.NormalizeSearchText); queries arrive compiled
-- (service.ParseBrainQuery), prefix and word items holding letters and
-- digits only.

-- name: ListNotesNeedingPassageIndex :many
-- Notes, archived ones included (a search may ask for them), whose passages
-- are missing, older than the note's revision, or cut by another chunker.
SELECT n.id, n.workspace_id, n.title, n.content, n.revision
FROM workspace_note n
WHERE (sqlc.narg('workspace_id')::uuid IS NULL OR n.workspace_id = sqlc.narg('workspace_id')::uuid)
  AND NOT EXISTS (
      SELECT 1 FROM workspace_note_passage p
      WHERE p.note_id = n.id
        AND p.ordinal = 1
        AND p.note_revision = n.revision
        AND p.chunker_version = sqlc.arg('chunker_version')::int
  )
ORDER BY n.updated_at DESC
LIMIT sqlc.arg('row_limit')::int;

-- name: ReplaceNotePassages :exec
-- One statement, so a search sees the old passages or the new ones, never a
-- mix: ordinals 1..n are upserted, ordinals above n deleted. A passage whose
-- content_hash is unchanged keeps its vector. The revision guard keeps a slow
-- indexer from overwriting what a newer revision already wrote.
WITH input AS (
    -- Set-returning functions in one select list zip arrays of equal length.
    SELECT unnest(sqlc.arg('ordinals')::int[]) AS ordinal,
           unnest(sqlc.arg('headings')::text[]) AS heading,
           unnest(sqlc.arg('bodies')::text[]) AS body,
           unnest(sqlc.arg('search_headings')::text[]) AS search_heading,
           unnest(sqlc.arg('search_bodies')::text[]) AS search_body,
           unnest(sqlc.arg('content_hashes')::text[]) AS content_hash
), stale AS (
    DELETE FROM workspace_note_passage old
    WHERE old.note_id = sqlc.arg('note_id')::uuid
      AND old.ordinal > cardinality(sqlc.arg('ordinals')::int[])
      AND old.note_revision <= sqlc.arg('note_revision')::bigint
)
INSERT INTO workspace_note_passage AS p (
    note_id, workspace_id, ordinal, heading, body, search_title, search_heading, search_body,
    note_revision, chunker_version, content_hash
)
SELECT sqlc.arg('note_id')::uuid, sqlc.arg('workspace_id')::uuid, i.ordinal, i.heading, i.body,
       sqlc.arg('search_title')::text, i.search_heading, i.search_body,
       sqlc.arg('note_revision')::bigint, sqlc.arg('chunker_version')::int, i.content_hash
FROM input i
ON CONFLICT (note_id, ordinal) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    heading = EXCLUDED.heading,
    body = EXCLUDED.body,
    search_title = EXCLUDED.search_title,
    search_heading = EXCLUDED.search_heading,
    search_body = EXCLUDED.search_body,
    note_revision = EXCLUDED.note_revision,
    chunker_version = EXCLUDED.chunker_version,
    embedding = CASE WHEN p.content_hash = EXCLUDED.content_hash THEN p.embedding END,
    embedding_model = CASE WHEN p.content_hash = EXCLUDED.content_hash THEN p.embedding_model END,
    embedded_at = CASE WHEN p.content_hash = EXCLUDED.content_hash THEN p.embedded_at END,
    content_hash = EXCLUDED.content_hash,
    indexed_at = now()
WHERE EXCLUDED.note_revision >= p.note_revision;

-- name: ListPassagesNeedingEmbedding :many
-- Passages of live notes, indexed at the note's current revision, with no
-- vector or one from another model; optionally for one note.
SELECT p.note_id, p.ordinal, p.heading, p.body, p.content_hash, n.title
FROM workspace_note_passage p
JOIN workspace_note n ON n.id = p.note_id
WHERE n.archived_at IS NULL
  AND p.note_revision = n.revision
  AND (sqlc.narg('note_id')::uuid IS NULL OR p.note_id = sqlc.narg('note_id')::uuid)
  AND (p.embedding IS NULL OR p.embedding_model IS DISTINCT FROM sqlc.arg('embedding_model')::text)
ORDER BY n.updated_at DESC, p.note_id, p.ordinal
LIMIT sqlc.arg('row_limit')::int;

-- name: SetPassageEmbedding :exec
-- A passage re-cut while its vector was computed has another content_hash
-- and keeps waiting for its own vector.
UPDATE workspace_note_passage
SET embedding = CAST(sqlc.arg('embedding')::text AS vector),
    embedding_model = sqlc.arg('embedding_model')::text,
    embedded_at = now()
WHERE note_id = sqlc.arg('note_id')::uuid
  AND ordinal = sqlc.arg('ordinal')::int
  AND content_hash = sqlc.arg('content_hash')::text;

-- name: DeleteOrphanNotePassages :execrows
-- Passages whose note no longer exists: an indexer that read a note just
-- before it was deleted.
-- ponytail: anti-join over the whole passage table per backfill tick; keep
-- a deletion log if the Brain grows to millions of passages.
DELETE FROM workspace_note_passage
WHERE note_id IN (
    SELECT DISTINCT o.note_id
    FROM workspace_note_passage o
    WHERE NOT EXISTS (SELECT 1 FROM workspace_note n WHERE n.id = o.note_id)
    LIMIT sqlc.arg('row_limit')::int
);

-- name: PurgeWorkspaceNotePassages :exec
DELETE FROM workspace_note_passage WHERE workspace_id = $1;

-- name: SearchBrainNotes :many
-- Ranked note search over passages. Each query item compiles to its own
-- tsquery (prefix, word or phrase); their OR is the GIN prefilter.
--
-- Lexical leg, per note: an item is "present" when some passage in scope
-- matches it (a word no note uses, or a CJK bigram straddling two words,
-- does not count against anyone). A note is admitted when it matches at
-- least half the present items (every one when strict), and never when one
-- of its passages matches an excluded item. Rank: items matched, best
-- passage rank, pinned, recency.
--
-- Vector leg: nearest passage per note, same model only. The two ranks fuse
-- by reciprocal rank (k = 60). The vector leg always has neighbours, however
-- unrelated the query, so a note only it finds is admitted only for capture
-- merge candidates (vector_only_hits, which a model judges) or above the
-- similarity floor calibrated for the model (vector_min_similarity).
--
-- The passage returned is the best lexical one, else the nearest.
WITH items AS MATERIALIZED (
    SELECT i.ord,
           CASE i.kind
               WHEN 'prefix' THEN to_tsquery('simple', i.txt || ':*')
               WHEN 'word' THEN to_tsquery('simple', i.txt)
               ELSE phraseto_tsquery('simple', i.txt)
           END AS q
    FROM (
        SELECT unnest(sqlc.arg('item_texts')::text[]) AS txt,
               unnest(sqlc.arg('item_kinds')::text[]) AS kind,
               generate_series(1, cardinality(sqlc.arg('item_texts')::text[])) AS ord
    ) i
), terms AS MATERIALIZED (
    SELECT it.ord, it.q FROM items it WHERE numnode(it.q) > 0
), q_or AS MATERIALIZED (
    SELECT string_agg('(' || t.q::text || ')', ' | ')::tsquery AS q FROM terms t
), excluded AS MATERIALIZED (
    SELECT e.q FROM (
        SELECT CASE x.kind
                   WHEN 'word' THEN to_tsquery('simple', x.txt)
                   ELSE phraseto_tsquery('simple', x.txt)
               END AS q
        FROM (
            SELECT unnest(sqlc.arg('excluded_texts')::text[]) AS txt,
                   unnest(sqlc.arg('excluded_kinds')::text[]) AS kind
        ) x
    ) e
    WHERE numnode(e.q) > 0
), excluded_notes AS MATERIALIZED (
    SELECT DISTINCT xp.note_id
    FROM excluded e
    JOIN workspace_note_passage xp ON xp.tsv @@ e.q
    WHERE xp.workspace_id = sqlc.arg('workspace_id')::uuid
), hits AS MATERIALIZED (
    SELECT p.note_id, p.ordinal, p.tsv, ts_rank_cd(p.tsv, (SELECT q FROM q_or)) AS rank
    FROM workspace_note_passage p
    JOIN workspace_note n ON n.id = p.note_id
    WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
      AND n.workspace_id = sqlc.arg('workspace_id')::uuid
      AND (sqlc.arg('include_archived')::bool OR n.archived_at IS NULL)
      AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY(n.tags))
      AND p.tsv @@ (SELECT q FROM q_or)
      AND p.note_id NOT IN (SELECT en.note_id FROM excluded_notes en)
), present AS MATERIALIZED (
    SELECT t.ord, t.q FROM terms t
    WHERE EXISTS (SELECT 1 FROM hits h WHERE h.tsv @@ t.q)
), lex_notes AS (
    SELECT h.note_id, count(DISTINCT pr.ord) AS matched, max(h.rank) AS best_rank
    FROM hits h
    JOIN present pr ON h.tsv @@ pr.q
    GROUP BY h.note_id
), lexical AS (
    SELECT l.note_id,
           row_number() OVER (ORDER BY l.matched DESC, l.best_rank DESC, ln.pinned DESC, ln.updated_at DESC) AS lex_rank
    FROM lex_notes l
    JOIN workspace_note ln ON ln.id = l.note_id
    WHERE l.matched >= CASE
        WHEN sqlc.arg('strict')::bool THEN (SELECT count(*) FROM present)
        ELSE GREATEST(1, ceil((SELECT count(*) FROM present) / 2.0))
    END
    ORDER BY lex_rank
    LIMIT sqlc.arg('prefilter')::int
), best_lexical AS (
    SELECT DISTINCT ON (h.note_id) h.note_id, h.ordinal
    FROM hits h
    WHERE h.note_id IN (SELECT lx.note_id FROM lexical lx)
    ORDER BY h.note_id, h.rank DESC, h.ordinal
), nearest AS (
    SELECT vp.note_id, vp.ordinal, vp.embedding <=> CAST(sqlc.narg('query_embedding')::text AS vector) AS distance
    FROM workspace_note_passage vp
    JOIN workspace_note vn ON vn.id = vp.note_id
    WHERE sqlc.narg('query_embedding')::text IS NOT NULL
      AND vp.workspace_id = sqlc.arg('workspace_id')::uuid
      AND vn.workspace_id = sqlc.arg('workspace_id')::uuid
      AND vp.embedding IS NOT NULL
      AND vp.embedding_model = sqlc.narg('embedding_model')::text
      AND (sqlc.arg('include_archived')::bool OR vn.archived_at IS NULL)
      AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY(vn.tags))
      AND vp.note_id NOT IN (SELECT en.note_id FROM excluded_notes en)
    ORDER BY distance
    LIMIT sqlc.arg('prefilter')::int * 4
), vec AS (
    SELECT d.note_id, d.ordinal, d.distance, row_number() OVER (ORDER BY d.distance) AS vec_rank
    FROM (
        SELECT DISTINCT ON (nr.note_id) nr.note_id, nr.ordinal, nr.distance
        FROM nearest nr
        ORDER BY nr.note_id, nr.distance
    ) d
    ORDER BY vec_rank
    LIMIT sqlc.arg('prefilter')::int
), fused AS (
    SELECT COALESCE(l.note_id, v.note_id) AS note_id,
           (COALESCE(1.0 / (60 + l.lex_rank), 0) + COALESCE(1.0 / (60 + v.vec_rank), 0))::float8 AS score,
           l.lex_rank, v.vec_rank, v.ordinal AS vec_ordinal
    FROM lexical l
    FULL OUTER JOIN vec v ON v.note_id = l.note_id
    WHERE l.note_id IS NOT NULL
       OR sqlc.arg('vector_only_hits')::bool
       OR 1 - v.distance >= sqlc.narg('vector_min_similarity')::float8
)
SELECT n.*, f.score, f.lex_rank, f.vec_rank,
       COALESCE(bp.heading, '')::text AS passage_heading,
       COALESCE(bp.body, '')::text AS passage_body
FROM fused f
JOIN workspace_note n ON n.id = f.note_id
LEFT JOIN best_lexical bl ON bl.note_id = f.note_id
LEFT JOIN workspace_note_passage bp ON bp.note_id = f.note_id AND bp.ordinal = COALESCE(bl.ordinal, f.vec_ordinal)
ORDER BY f.score DESC, n.pinned DESC, n.updated_at DESC
LIMIT sqlc.arg('top_k')::int;
