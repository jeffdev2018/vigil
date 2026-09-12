-- Packs (OS plan, vague B): install ledger and per-row provenance.

-- name: CreatePackInstall :one
INSERT INTO workspace_pack_install (id, workspace_id, pack_id, pack_version, title, source, strategy, bundle_sha256, run_id, status, manifest, installed_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'installed', $10, $11)
RETURNING *;

-- name: FinishPackInstall :one
UPDATE workspace_pack_install SET status = $3, report = $4 WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: GetPackInstall :one
SELECT * FROM workspace_pack_install WHERE id = $1 AND workspace_id = $2;

-- name: GetInstalledPack :one
SELECT * FROM workspace_pack_install WHERE workspace_id = $1 AND pack_id = $2 AND status = 'installed'
ORDER BY installed_at DESC LIMIT 1;

-- name: ListPackInstalls :many
SELECT * FROM workspace_pack_install WHERE workspace_id = $1 ORDER BY installed_at DESC LIMIT 200;

-- name: MarkPackInstallRemoved :one
UPDATE workspace_pack_install SET status = 'removed', removed_by = $3, removed_at = now(), report = $4
WHERE id = $1 AND workspace_id = $2 AND status = 'installed' RETURNING *;

-- name: MarkPackInstallSuperseded :exec
UPDATE workspace_pack_install SET status = 'removed', removed_by = $3, removed_at = now()
WHERE workspace_id = $1 AND pack_id = $2 AND status = 'installed';

-- name: CreatePackItem :exec
INSERT INTO workspace_pack_item (id, install_id, workspace_id, kind, row_id, name, action) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListPackItems :many
SELECT * FROM workspace_pack_item WHERE install_id = $1 AND workspace_id = $2 ORDER BY created_at ASC, id ASC;

-- name: PurgeWorkspacePackInstalls :exec
DELETE FROM workspace_pack_install WHERE workspace_id = $1;

-- name: PurgeWorkspacePackItems :exec
DELETE FROM workspace_pack_item WHERE workspace_id = $1;

-- Lookups the pack apply needs for the kinds the transfer bundle did not carry.

-- name: GetIssuePropertyByNameForImport :one
SELECT * FROM issue_property WHERE workspace_id = $1 AND lower(name) = lower($2) AND archived_at IS NULL LIMIT 1;

-- name: GetIssueViewByNameForImport :one
SELECT * FROM issue_view WHERE workspace_id = $1 AND name = $2 AND visibility = 'workspace' LIMIT 1;

-- name: ListWorkspaceIssueViewsForExport :many
SELECT * FROM issue_view WHERE workspace_id = $1 AND visibility = 'workspace' AND scope_type IN ('workspace', 'project') ORDER BY created_at ASC;

-- name: GetIssueTransitionRuleForImport :one
SELECT * FROM issue_transition_rule
WHERE workspace_id = $1 AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')::uuid
  AND from_category IS NOT DISTINCT FROM sqlc.narg('from_category')::text AND to_category = $2
LIMIT 1;

-- name: GetBusinessRuleByTitleForImport :one
SELECT * FROM business_rule WHERE workspace_id = $1 AND title = $2 LIMIT 1;

-- name: GetModuleOwnershipForImport :one
SELECT * FROM module_ownership
WHERE workspace_id = $1 AND path_pattern IS NOT DISTINCT FROM sqlc.narg('path_pattern')::text AND label_id IS NOT DISTINCT FROM sqlc.narg('label_id')::uuid
LIMIT 1;
