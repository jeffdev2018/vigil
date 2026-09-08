ALTER TABLE issue_dependency DROP COLUMN IF EXISTS created_at;
ALTER TABLE issue DROP COLUMN IF EXISTS issue_type;
DROP TABLE IF EXISTS issue_property_type;
DROP TABLE IF EXISTS issue_type;
