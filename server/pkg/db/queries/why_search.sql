-- Why search (K55).

-- name: UpsertWhyChunk :exec
INSERT INTO decision_search_chunk (id, workspace_id, source_type, source_id, issue_id, content)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (source_type, source_id) DO UPDATE SET content = EXCLUDED.content, issue_id = EXCLUDED.issue_id, updated_at = now();

-- name: DeleteWhyChunk :exec
DELETE FROM decision_search_chunk WHERE source_type = $1 AND source_id = $2;

-- name: SearchWhy :many
SELECT c.id, c.source_type, c.source_id, c.issue_id, c.created_at,
       ts_rank_cd(c.tsv, q)::float8 AS score,
       ts_headline('english', c.content, q, 'MaxWords=40, MinWords=15, MaxFragments=2, FragmentDelimiter= … ')::text AS snippet,
       i.number AS issue_number, i.title AS issue_title
FROM decision_search_chunk c
CROSS JOIN websearch_to_tsquery('english', sqlc.arg(query)::text) q
LEFT JOIN issue i ON i.id = c.issue_id
WHERE c.workspace_id = $1 AND c.tsv @@ q
ORDER BY score DESC, c.created_at DESC
LIMIT 20;

-- name: PurgeWorkspaceWhyChunks :exec
DELETE FROM decision_search_chunk WHERE workspace_id = $1;

-- Reindex sources.

-- name: ListWorkspaceCommentsForWhy :many
SELECT id, issue_id, content FROM comment
WHERE workspace_id = $1 AND length(content) >= 20
ORDER BY created_at DESC LIMIT 5000;

-- name: ListWorkspaceDecisionRecordsForWhy :many
SELECT id, issue_id, title, context, decision FROM decision_record
WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 5000;

-- name: ListWorkspaceTextMessagesForWhy :many
SELECT m.id, t.issue_id, m.content
FROM task_message m
JOIN agent_task_queue t ON t.id = m.task_id
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = $1 AND m.type = 'text' AND t.issue_id IS NOT NULL AND length(m.content) >= 40
ORDER BY m.created_at DESC LIMIT 5000;
