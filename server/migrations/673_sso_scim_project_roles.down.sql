ALTER TABLE member DROP COLUMN IF EXISTS scim_external_id;
ALTER TABLE "user" DROP COLUMN IF EXISTS sessions_invalidated_at;
DROP TABLE IF EXISTS project_member_role;
DROP TABLE IF EXISTS scim_token;
DROP TABLE IF EXISTS workspace_sso_connection;
