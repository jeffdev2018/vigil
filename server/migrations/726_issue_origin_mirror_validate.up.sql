-- Validate the widened issue_origin_type_check that 725 added NOT VALID.
-- 725 only WIDENED the allowed set, so no pre-existing row can fail the scan.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
