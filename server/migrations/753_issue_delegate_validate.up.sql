-- Validate the CHECK 752 added NOT VALID. The column was introduced empty by
-- the same change, so no pre-existing row can fail the scan; VALIDATE runs
-- under SHARE UPDATE EXCLUSIVE and does not block concurrent reads or writes.
ALTER TABLE issue VALIDATE CONSTRAINT issue_delegate_type_check;
