ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_delegate_type_check;
ALTER TABLE issue DROP COLUMN IF EXISTS delegate_id;
ALTER TABLE issue DROP COLUMN IF EXISTS delegate_type;
