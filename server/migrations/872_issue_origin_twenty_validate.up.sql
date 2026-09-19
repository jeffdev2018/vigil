-- Validate the widened issue_origin_type_check that 871 added NOT VALID.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
