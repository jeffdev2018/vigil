-- Workspace export / import (K76).

-- name: CreateWorkspaceTransferRun :one
INSERT INTO workspace_transfer_run (id, workspace_id, direction, status, name, template, strategy, source_name, bundle_sha256, bundle, report, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: FinishWorkspaceTransferRun :one
UPDATE workspace_transfer_run SET status = $2, report = $3, completed_at = now() WHERE id = $1 RETURNING *;

-- name: ListWorkspaceTransferRuns :many
SELECT id, workspace_id, direction, status, name, template, strategy, source_name, bundle_sha256, report, created_by, created_at, completed_at
FROM workspace_transfer_run WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 50;

-- name: GetWorkspaceTransferRun :one
SELECT * FROM workspace_transfer_run WHERE id = $1;

-- name: ListWorkspaceTemplatesForUser :many
-- Template exports of every workspace the user belongs to, for the
-- "start from a template" choice before any workspace exists.
SELECT r.id, r.workspace_id, r.name, r.source_name, r.report, r.created_at, w.name AS workspace_name
FROM workspace_transfer_run r
JOIN workspace w ON w.id = r.workspace_id
WHERE r.template AND r.direction = 'export' AND r.status = 'completed' AND r.bundle IS NOT NULL
  AND r.workspace_id IN (SELECT m.workspace_id FROM member m WHERE m.user_id = $1)
ORDER BY r.created_at DESC LIMIT 50;

-- name: GetAgentByNameForImport :one
SELECT * FROM agent WHERE workspace_id = $1 AND name = $2 AND archived_at IS NULL AND kind = 'user' LIMIT 1;

-- name: GetSkillByNameForImport :one
SELECT * FROM skill WHERE workspace_id = $1 AND name = $2 LIMIT 1;

-- name: GetProjectByTitleForImport :one
SELECT * FROM project WHERE workspace_id = $1 AND title = $2 LIMIT 1;

-- name: GetGoalByTitleForImport :one
SELECT * FROM goal WHERE workspace_id = $1 AND title = $2 AND status <> 'dropped' LIMIT 1;

-- name: GetAutopilotByTitleForImport :one
SELECT * FROM autopilot WHERE workspace_id = $1 AND title = $2 AND status <> 'archived' LIMIT 1;

-- name: GetPermissionProfileByNameForImport :one
SELECT * FROM agent_permission_profile WHERE workspace_id = $1 AND name = $2 LIMIT 1;

-- name: GetTriageSourceByNameForImport :one
SELECT * FROM triage_source WHERE workspace_id = $1 AND kind = $2 AND name = $3 LIMIT 1;

-- name: ListAutopilotsForExport :many
SELECT * FROM autopilot WHERE workspace_id = $1 AND status <> 'archived' ORDER BY created_at ASC;

-- name: ListWorkspaceNotesForExport :many
SELECT * FROM workspace_note WHERE workspace_id = $1 AND archived_at IS NULL AND merged_into IS NULL ORDER BY created_at ASC LIMIT 2000;

-- name: ListIssuesForExport :many
SELECT * FROM issue WHERE workspace_id = $1 ORDER BY number ASC LIMIT 5000;

-- name: ListLabelsForExport :many
SELECT il.issue_id, l.name FROM issue_to_label il JOIN issue_label l ON l.id = il.label_id
WHERE l.workspace_id = $1;

-- name: MergeImportedAgent :exec
UPDATE agent SET description = $3, instructions = $4, model = $5, thinking_level = $6, mcp_config = $7, runtime_config = $8,
    custom_args = $9, conversation_starters = $10, trust_mode = $11, effect_mode = $12, scoped_env_keys = $13, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: SetImportedAgentModes :exec
UPDATE agent SET trust_mode = $3, effect_mode = $4, scoped_env_keys = $5, permission_profile_id = $6, custom_env = $7 WHERE id = $1 AND workspace_id = $2;

-- name: CreateTriageSourceForImport :one
INSERT INTO triage_source (workspace_id, kind, ref_id, name, icon, mode, auto_accept, cap_per_hour, expiry_days, created_by_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: MergeImportedTriageSource :exec
UPDATE triage_source SET mode = $3, auto_accept = $4, cap_per_hour = $5, expiry_days = $6, updated_at = now() WHERE id = $1 AND workspace_id = $2;

-- name: MergeImportedProject :exec
UPDATE project SET description = $3, icon = $4, status = $5, priority = $6, updated_at = now() WHERE id = $1 AND workspace_id = $2;

-- name: SetIssueGoalForImport :exec
UPDATE issue SET goal_id = $3 WHERE id = $1 AND workspace_id = $2;

-- name: PurgeWorkspaceTransferRuns :exec
DELETE FROM workspace_transfer_run WHERE workspace_id = $1;

-- name: GetLabelByNameForImport :one
SELECT * FROM issue_label WHERE workspace_id = $1 AND resource_type = 'issue' AND name = $2 LIMIT 1;

-- name: CountMembersForTransfer :one
SELECT count(*) FROM member WHERE workspace_id = $1;

-- name: CountWorkspaceNotesByTitleForImport :one
SELECT COUNT(*) FROM workspace_note WHERE workspace_id = $1 AND title = $2;

-- name: CountIssuesByTitleForImport :one
SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = $2;
