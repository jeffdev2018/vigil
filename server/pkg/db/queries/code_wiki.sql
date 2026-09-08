-- F26 (JEF-22): the generated code wiki. A snapshot is claimed in `building`,
-- filled with pages, then published in one statement.

-- name: ClaimCodeWikiSnapshotBuild :one
-- Claim the single in-flight generation slot for a repo resource.
--
-- `code_wiki_snapshot_one_building_per_resource` is a partial unique index on
-- (project_resource_id) WHERE state = 'building', so this returns a row for the
-- first caller and nothing for every caller that arrives while a generation is
-- already running. That is what makes a burst of merges enqueue one run: only
-- the caller that gets a row dispatches.
INSERT INTO code_wiki_snapshot (workspace_id, project_resource_id, commit_sha, state, generated_by_task_id)
VALUES ($1, $2, $3, 'building', $4)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetBuildingCodeWikiSnapshot :one
SELECT * FROM code_wiki_snapshot
WHERE project_resource_id = $1 AND state = 'building';

-- name: AbandonStaleCodeWikiBuilds :execrows
-- A run that died without publishing would otherwise hold the build slot for
-- ever. Anything still `building` past the cutoff is marked failed so the next
-- merge can claim the slot again.
UPDATE code_wiki_snapshot SET state = 'failed'
WHERE state = 'building' AND created_at < $1;

-- name: AnnounceCodeWikiSnapshotInventory :one
-- Record the commit and the file inventory the generating run announced.
-- `repo_paths` is the set every page citation is checked against.
UPDATE code_wiki_snapshot SET
    commit_sha = $3,
    repo_paths = $4,
    generated_by_task_id = COALESCE($5, generated_by_task_id)
WHERE id = $1 AND workspace_id = $2 AND state = 'building'
RETURNING *;

-- name: GetCodeWikiSnapshot :one
SELECT * FROM code_wiki_snapshot
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertCodeWikiPage :one
INSERT INTO code_wiki_page (workspace_id, snapshot_id, project_resource_id, slug, title, content, citations)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (snapshot_id, slug) DO UPDATE SET
    title = EXCLUDED.title,
    content = EXCLUDED.content,
    citations = EXCLUDED.citations
RETURNING *;

-- name: PublishCodeWikiSnapshot :one
-- The whole point of the snapshot: one statement flips a fully written build to
-- `published` and stamps its page count. A run that dies before this leaves the
-- previous published snapshot untouched and still visible, because readers only
-- ever select the newest `published` row.
UPDATE code_wiki_snapshot SET
    state = 'published',
    published_at = now(),
    page_count = (SELECT count(*) FROM code_wiki_page pg WHERE pg.snapshot_id = $1)
WHERE code_wiki_snapshot.id = $1 AND code_wiki_snapshot.workspace_id = $2 AND code_wiki_snapshot.state = 'building'
RETURNING *;

-- name: FailCodeWikiSnapshot :execrows
UPDATE code_wiki_snapshot SET state = 'failed'
WHERE id = $1 AND workspace_id = $2 AND state = 'building';

-- name: DeleteCodeWikiPagesOutsideNewest :exec
-- History is the current snapshot plus the one before it, no further: the spec
-- excludes deeper history, and pages are the bulk of the bytes.
DELETE FROM code_wiki_page WHERE snapshot_id IN (
    SELECT s.id FROM code_wiki_snapshot s
    WHERE s.project_resource_id = $1 AND s.state = 'published'
    ORDER BY s.published_at DESC
    OFFSET 2
);

-- name: DeleteCodeWikiSnapshotsOutsideNewest :exec
DELETE FROM code_wiki_snapshot WHERE id IN (
    SELECT s.id FROM code_wiki_snapshot s
    WHERE s.project_resource_id = $1 AND s.state = 'published'
    ORDER BY s.published_at DESC
    OFFSET 2
);

-- name: GetPublishedCodeWikiSnapshot :one
-- The visible wiki for a resource: newest published snapshot, plus the newest
-- commit we were ever told about for that resource in `head_commit_sha`. A
-- served snapshot whose commit differs from that head is stale.
SELECT sqlc.embed(s),
       (SELECT h.commit_sha FROM code_wiki_snapshot h
        WHERE h.project_resource_id = s.project_resource_id
        ORDER BY h.created_at DESC LIMIT 1)::text AS head_commit_sha
FROM code_wiki_snapshot s
WHERE s.project_resource_id = $1 AND s.workspace_id = $2 AND s.state = 'published'
ORDER BY s.published_at DESC
LIMIT 1;

-- name: ListCodeWikiPageSummaries :many
-- Table of contents. Content is excluded on purpose — a snapshot's pages are
-- Markdown documents and the sidebar only needs their titles.
SELECT id, slug, title, citations, created_at
FROM code_wiki_page
WHERE snapshot_id = $1
ORDER BY title ASC, slug ASC;

-- name: GetCodeWikiPageInSnapshot :one
SELECT * FROM code_wiki_page
WHERE snapshot_id = $1 AND slug = $2;

-- name: SearchCodeWikiPages :many
-- V1 search is a case-insensitive LIKE over the newest published snapshot of
-- every repo resource in the workspace. No trigram index: pg_bigm is only
-- conditionally present in this deployment, so an index on it cannot be relied
-- on, and a wiki is tens of pages per repo.
WITH latest AS (
    SELECT DISTINCT ON (s.project_resource_id)
           s.id, s.project_resource_id, s.commit_sha, s.published_at
    FROM code_wiki_snapshot s
    WHERE s.workspace_id = $1 AND s.state = 'published'
    ORDER BY s.project_resource_id, s.published_at DESC
), head AS (
    SELECT DISTINCT ON (h.project_resource_id) h.project_resource_id, h.commit_sha
    FROM code_wiki_snapshot h
    WHERE h.workspace_id = $1
    ORDER BY h.project_resource_id, h.created_at DESC
)
SELECT p.id, p.slug, p.title, p.content, p.citations,
       l.project_resource_id, l.commit_sha,
       COALESCE(head.commit_sha, l.commit_sha)::text AS head_commit_sha
FROM code_wiki_page p
JOIN latest l ON l.id = p.snapshot_id
LEFT JOIN head ON head.project_resource_id = l.project_resource_id
WHERE (sqlc.narg('project_resource_id')::uuid IS NULL OR l.project_resource_id = sqlc.narg('project_resource_id')::uuid)
  AND (LOWER(p.title) LIKE sqlc.arg('pattern')::text OR LOWER(p.content) LIKE sqlc.arg('pattern')::text)
ORDER BY (LOWER(p.title) LIKE sqlc.arg('pattern')::text) DESC, p.title ASC
LIMIT sqlc.arg('result_limit');

-- name: FindCodeWikiPageBySlug :one
-- Slug lookup for the MCP tool: newest published snapshot in the workspace that
-- carries this slug, optionally narrowed to one repo resource.
WITH latest AS (
    SELECT DISTINCT ON (s.project_resource_id)
           s.id, s.project_resource_id, s.commit_sha, s.published_at
    FROM code_wiki_snapshot s
    WHERE s.workspace_id = $1 AND s.state = 'published'
    ORDER BY s.project_resource_id, s.published_at DESC
), head AS (
    SELECT DISTINCT ON (h.project_resource_id) h.project_resource_id, h.commit_sha
    FROM code_wiki_snapshot h
    WHERE h.workspace_id = $1
    ORDER BY h.project_resource_id, h.created_at DESC
)
SELECT p.id, p.slug, p.title, p.content, p.citations,
       l.project_resource_id, l.commit_sha,
       COALESCE(head.commit_sha, l.commit_sha)::text AS head_commit_sha
FROM code_wiki_page p
JOIN latest l ON l.id = p.snapshot_id
LEFT JOIN head ON head.project_resource_id = l.project_resource_id
WHERE p.slug = sqlc.arg('slug')
  AND (sqlc.narg('project_resource_id')::uuid IS NULL OR l.project_resource_id = sqlc.narg('project_resource_id')::uuid)
ORDER BY l.published_at DESC
LIMIT 1;

-- name: DeleteCodeWikiPagesByResource :exec
DELETE FROM code_wiki_page WHERE project_resource_id = $1;

-- name: DeleteCodeWikiSnapshotsByResource :exec
DELETE FROM code_wiki_snapshot WHERE project_resource_id = $1;

-- name: ListPublishedCodeWikiResourceIDs :many
SELECT DISTINCT project_resource_id FROM code_wiki_snapshot
WHERE workspace_id = $1 AND state = 'published';

-- name: UpsertCodeWikiMcpServer :one
-- Auto-registration (acceptance 6). Idempotent on the (workspace_id, name)
-- unique index: republishing cannot create a second library row.
INSERT INTO workspace_mcp_server (workspace_id, name, config)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, name) DO UPDATE SET
    config = EXCLUDED.config,
    updated_at = now()
RETURNING *;

-- name: FindCodeWikiAutopilotForProject :one
-- The project's wiki autopilot: active, and carrying an enabled webhook trigger
-- with the label the wiki daemon declares. Installing that daemon is what opts a
-- project in — a workspace that never did is never charged for a run.
SELECT sqlc.embed(a), sqlc.embed(t)
FROM autopilot a
JOIN autopilot_trigger t ON t.autopilot_id = a.id
WHERE a.workspace_id = $1
  AND a.project_id = $2
  AND a.status = 'active'
  AND t.enabled = TRUE
  AND t.kind = 'webhook'
  AND LOWER(TRIM(BOTH FROM COALESCE(t.label, ''))) = $3
ORDER BY a.created_at ASC
LIMIT 1;

-- name: ListWorkspaceGithubRepoResources :many
-- Every repo resource in a workspace, for matching a merged pull request back
-- to the projects that track that repository.
SELECT * FROM project_resource
WHERE workspace_id = $1 AND resource_type = 'github_repo';
