-- K60 · SSO, SCIM and project roles.

-- name: UpsertSSOConnection :one
INSERT INTO workspace_sso_connection (id, workspace_id, provider, issuer, client_id, client_secret_encrypted, allowed_domains, auto_provision, enforced, created_by)
VALUES ($1, $2, 'oidc', $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (workspace_id) DO UPDATE SET
    issuer = EXCLUDED.issuer, client_id = EXCLUDED.client_id,
    client_secret_encrypted = CASE WHEN EXCLUDED.client_secret_encrypted = '' THEN workspace_sso_connection.client_secret_encrypted ELSE EXCLUDED.client_secret_encrypted END,
    allowed_domains = EXCLUDED.allowed_domains, auto_provision = EXCLUDED.auto_provision, enforced = EXCLUDED.enforced, updated_at = now()
RETURNING *;

-- name: GetSSOConnection :one
SELECT * FROM workspace_sso_connection WHERE workspace_id = $1;

-- name: SetSSOEnforced :one
UPDATE workspace_sso_connection SET enforced = $2, updated_at = now() WHERE workspace_id = $1 RETURNING *;

-- name: DeleteSSOConnection :exec
DELETE FROM workspace_sso_connection WHERE workspace_id = $1;

-- name: ListEnforcedSSOConnections :many
-- Every enforced connection with its workspace slug: code and Google logins
-- check the caller's memberships and email domain against them.
SELECT c.*, w.slug AS workspace_slug FROM workspace_sso_connection c
JOIN workspace w ON w.id = c.workspace_id
WHERE c.enforced = TRUE;

-- name: CreateScimToken :one
INSERT INTO scim_token (id, workspace_id, token_hash, token_hint, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetActiveScimTokenByHash :one
SELECT * FROM scim_token WHERE token_hash = $1 AND active = TRUE;

-- name: ListScimTokens :many
SELECT * FROM scim_token WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: DeactivateScimTokens :exec
UPDATE scim_token SET active = FALSE WHERE workspace_id = $1 AND active = TRUE;

-- name: DeactivateScimToken :execrows
UPDATE scim_token SET active = FALSE WHERE id = $1 AND workspace_id = $2 AND active = TRUE;

-- name: TouchScimToken :exec
UPDATE scim_token SET last_used_at = now() WHERE id = $1;

-- name: InvalidateUserSessions :exec
UPDATE "user" SET sessions_invalidated_at = now(), updated_at = now() WHERE id = $1;

-- name: GetUserSessionsInvalidatedAt :one
SELECT sessions_invalidated_at FROM "user" WHERE id = $1;

-- name: SetMemberScimExternalID :exec
UPDATE member SET scim_external_id = $2 WHERE id = $1;

-- name: GetMemberByScimExternalID :one
SELECT * FROM member WHERE workspace_id = $1 AND scim_external_id = $2;

-- name: GetMemberByID :one
SELECT * FROM member WHERE id = $1 AND workspace_id = $2;

-- name: UpsertProjectMemberRole :one
INSERT INTO project_member_role (id, workspace_id, project_id, subject_type, subject_id, role, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (project_id, subject_type, subject_id) DO UPDATE SET role = EXCLUDED.role, updated_at = now()
RETURNING *;

-- name: DeleteProjectMemberRole :execrows
DELETE FROM project_member_role WHERE project_id = $1 AND subject_type = $2 AND subject_id = $3;

-- name: ListProjectMemberRoles :many
SELECT * FROM project_member_role WHERE project_id = $1;

-- name: GetProjectMemberRole :one
SELECT * FROM project_member_role WHERE project_id = $1 AND subject_type = $2 AND subject_id = $3;

-- name: PurgeWorkspaceSSOConnections :exec
DELETE FROM workspace_sso_connection WHERE workspace_id = $1;

-- name: PurgeWorkspaceScimTokens :exec
DELETE FROM scim_token WHERE workspace_id = $1;

-- name: PurgeWorkspaceProjectMemberRoles :exec
DELETE FROM project_member_role WHERE workspace_id = $1;

-- name: SetUserName :exec
UPDATE "user" SET name = $2, updated_at = now() WHERE id = $1;
