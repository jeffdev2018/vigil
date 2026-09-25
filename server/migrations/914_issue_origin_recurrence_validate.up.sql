-- Validate the widened issue_origin_type_check that 913 added NOT VALID.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
