-- Epic Mode (F18): the versioned artifacts of a project's PRD -> tech plan ->
-- wireframe -> tickets pipeline.
--
-- One row per publication. The rows of a kind are ordered by version; the
-- newest non-claim row is what the API calls `latest`, and at most one row per
-- kind is `approved`. A row whose `content` is empty is a GENERATION CLAIM —
-- the version an in-flight run reserved. Nothing else may write an empty
-- content, which is what makes the emptiness a reliable discriminator.

-- name: ListEpicArtifacts :many
SELECT * FROM epic_artifact
WHERE project_id = $1 AND workspace_id = $2
ORDER BY kind ASC, version DESC;

-- name: GetEpicArtifactByID :one
SELECT * FROM epic_artifact WHERE id = $1 AND workspace_id = $2;

-- name: GetEpicArtifactByTask :one
-- ponytail: unindexed lookup on a table that holds a handful of rows per
-- project; add an index on generated_by_task_id if it ever grows.
SELECT * FROM epic_artifact WHERE generated_by_task_id = $1;

-- name: GetProjectEpicIssue :one
-- The host issue is created once per project and copied onto every later row.
SELECT epic_issue_id FROM epic_artifact
WHERE project_id = $1 AND epic_issue_id IS NOT NULL
ORDER BY created_at ASC
LIMIT 1;

-- name: ClaimEpicStepGeneration :one
-- Reserves the next version of a kind for an in-flight run. The version is
-- computed in the statement; the unique (project_id, kind, version) index of
-- migration 762 turns a concurrent generate into an error the handler answers
-- with 409 rather than a second claim.
INSERT INTO epic_artifact (workspace_id, project_id, kind, version, content, state, epic_issue_id, author_type, author_id)
SELECT sqlc.arg(workspace_id), sqlc.arg(project_id), sqlc.arg(kind),
       COALESCE(MAX(version), 0) + 1, '', 'draft',
       sqlc.narg(epic_issue_id), sqlc.arg(author_type), sqlc.arg(author_id)
FROM epic_artifact
WHERE project_id = sqlc.arg(project_id) AND kind = sqlc.arg(kind)
RETURNING *;

-- name: CreateEpicArtifact :one
-- A finished publication: a human edit, or a settled generation whose claim
-- could not be reused.
INSERT INTO epic_artifact (workspace_id, project_id, kind, version, content, payload, state, epic_issue_id, generated_by_task_id, author_type, author_id)
SELECT sqlc.arg(workspace_id), sqlc.arg(project_id), sqlc.arg(kind),
       COALESCE(MAX(version), 0) + 1,
       sqlc.arg(content), sqlc.arg(payload), 'draft',
       sqlc.narg(epic_issue_id), sqlc.narg(generated_by_task_id),
       sqlc.arg(author_type), sqlc.arg(author_id)
FROM epic_artifact
WHERE project_id = sqlc.arg(project_id) AND kind = sqlc.arg(kind)
RETURNING *;

-- name: SetEpicArtifactTask :exec
UPDATE epic_artifact SET generated_by_task_id = $2, updated_at = now() WHERE id = $1;

-- name: SettleEpicArtifact :one
-- Fills a claim with what the run answered. `content = ''` in the predicate is
-- the idempotency guard: a replayed completion matches no row.
UPDATE epic_artifact
SET content = sqlc.arg(content), payload = sqlc.arg(payload), updated_at = now()
WHERE id = sqlc.arg(id) AND content = ''
RETURNING *;

-- name: DeleteEpicArtifact :exec
-- Drops a claim whose run never produced a readable block, so the step is not
-- left generating forever.
DELETE FROM epic_artifact WHERE id = $1 AND content = '';

-- name: SupersedeOtherEpicArtifacts :exec
UPDATE epic_artifact SET state = 'superseded', updated_at = now()
WHERE project_id = $1 AND kind = $2 AND id <> $3 AND state <> 'superseded';

-- name: SupersedeEpicArtifactKinds :exec
-- Editing or regenerating an approved step re-opens the gate: every LATER
-- step's rows become superseded, so nothing downstream still reads approved
-- against a premise that changed.
UPDATE epic_artifact SET state = 'superseded', updated_at = now()
WHERE project_id = $1 AND kind = ANY(sqlc.arg(kinds)::text[]) AND state <> 'superseded';

-- name: ApproveEpicArtifact :one
UPDATE epic_artifact
SET state = 'approved', approved_by = sqlc.narg(approved_by), approved_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND state = 'draft' AND content <> ''
RETURNING *;

-- name: SetEpicArtifactPayload :one
UPDATE epic_artifact SET payload = $2, updated_at = now() WHERE id = $1 RETURNING *;
