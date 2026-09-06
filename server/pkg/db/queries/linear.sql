-- Linear Bridge (K21): the installation, the issue mirror links, and the
-- comment links that keep a mirrored comment from bouncing back.

-- name: UpsertLinearInstallation :one
INSERT INTO linear_installation (
    workspace_id, agent_id, linear_org_id, linear_org_name, actor_user_id,
    access_token_encrypted, webhook_secret_encrypted, linear_webhook_id,
    status_map, status, last_error, installed_by
) VALUES (
    sqlc.arg(workspace_id), sqlc.arg(agent_id), sqlc.arg(linear_org_id), sqlc.arg(linear_org_name), sqlc.arg(actor_user_id),
    sqlc.arg(access_token_encrypted), sqlc.narg(webhook_secret_encrypted), sqlc.arg(linear_webhook_id),
    sqlc.arg(status_map), 'active', '', sqlc.narg(installed_by)
)
ON CONFLICT (workspace_id) DO UPDATE SET
    agent_id = EXCLUDED.agent_id,
    linear_org_id = EXCLUDED.linear_org_id,
    linear_org_name = EXCLUDED.linear_org_name,
    actor_user_id = EXCLUDED.actor_user_id,
    access_token_encrypted = EXCLUDED.access_token_encrypted,
    webhook_secret_encrypted = EXCLUDED.webhook_secret_encrypted,
    linear_webhook_id = EXCLUDED.linear_webhook_id,
    status_map = EXCLUDED.status_map,
    status = 'active',
    last_error = '',
    installed_by = EXCLUDED.installed_by,
    updated_at = now()
RETURNING *;

-- name: GetLinearInstallationByWorkspace :one
SELECT * FROM linear_installation WHERE workspace_id = $1;

-- name: GetLinearInstallation :one
SELECT * FROM linear_installation WHERE id = $1;

-- name: ListLinearInstallationsByOrg :many
-- Webhook routing. The same Linear org can be connected from more than one
-- Multica workspace, so the payload's organizationId narrows the candidates
-- and the per-installation signature picks the one that actually sent it.
SELECT * FROM linear_installation
WHERE linear_org_id = $1 AND status <> 'revoked'
ORDER BY created_at;

-- name: SetLinearInstallationStatus :exec
UPDATE linear_installation
SET status = sqlc.arg(status), last_error = sqlc.arg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: UpdateLinearInstallationStatusMap :one
UPDATE linear_installation
SET status_map = sqlc.arg(status_map), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteLinearInstallation :exec
DELETE FROM linear_installation WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: CreateLinearIssueLink :one
INSERT INTO linear_issue_link (
    workspace_id, installation_id, issue_id, linear_issue_id,
    linear_issue_identifier, linear_team_id, linear_url, last_synced_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (linear_team_id, linear_issue_id) DO UPDATE SET
    last_synced_at = now()
RETURNING *;

-- name: GetLinearIssueLinkByRemote :one
SELECT * FROM linear_issue_link
WHERE linear_team_id = sqlc.arg(linear_team_id) AND linear_issue_id = sqlc.arg(linear_issue_id);

-- name: GetLinearIssueLinkByIssue :one
SELECT * FROM linear_issue_link WHERE issue_id = $1 ORDER BY created_at LIMIT 1;

-- name: GetLinearIssueLinkInWorkspace :one
SELECT * FROM linear_issue_link WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: SetLinearIssueLinkState :exec
UPDATE linear_issue_link
SET sync_state = sqlc.arg(sync_state), last_error = sqlc.arg(last_error), last_synced_at = now()
WHERE id = sqlc.arg(id);

-- name: CreateLinearCommentLink :exec
-- ON CONFLICT DO NOTHING makes a redelivered webhook idempotent: the second
-- delivery finds the row already there and mirrors nothing.
INSERT INTO linear_comment_link (installation_id, comment_id, linear_comment_id, direction)
VALUES ($1, $2, $3, $4)
ON CONFLICT (linear_comment_id) DO NOTHING;

-- name: GetLinearCommentLinkByComment :one
SELECT * FROM linear_comment_link WHERE comment_id = $1;

-- name: GetLinearCommentLinkByRemote :one
SELECT * FROM linear_comment_link WHERE linear_comment_id = $1;

-- name: PurgeWorkspaceLinearCommentLinks :exec
DELETE FROM linear_comment_link
WHERE installation_id IN (SELECT id FROM linear_installation WHERE workspace_id = $1);

-- name: PurgeWorkspaceLinearIssueLinks :exec
DELETE FROM linear_issue_link WHERE workspace_id = $1;

-- name: PurgeWorkspaceLinearInstallations :exec
DELETE FROM linear_installation WHERE workspace_id = $1;

-- name: GetLinearIssueLinkByRemoteIssue :one
-- Comment deliveries carry the Linear issue id but not always its team, and a
-- Linear issue id is globally unique, so the installation scopes the lookup.
SELECT * FROM linear_issue_link
WHERE installation_id = sqlc.arg(installation_id) AND linear_issue_id = sqlc.arg(linear_issue_id);

-- name: UpdateLinearMirrorIssueContent :one
-- Linear owns the title and description of a mirrored issue, so an inbound
-- update overwrites them. Narrow on purpose: it must not be able to touch
-- assignee, project or anything else Multica owns.
UPDATE issue SET
    title = sqlc.arg(title),
    description = sqlc.arg(description),
    revision = revision + 1,
    last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now()),
    updated_at = now()
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;
