DROP TABLE IF EXISTS workspace_doctrine_report;
DROP TABLE IF EXISTS workspace_doctrine_version;
ALTER TABLE workspace DROP COLUMN IF EXISTS doctrine_updated_by;
ALTER TABLE workspace DROP COLUMN IF EXISTS doctrine_updated_at;
ALTER TABLE workspace DROP COLUMN IF EXISTS doctrine_revision;
