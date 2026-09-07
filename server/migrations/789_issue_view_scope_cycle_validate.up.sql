-- Validate the three constraints 788 widened NOT VALID. 788 only ADDED
-- accepted values, so no pre-existing row can fail the scan.
ALTER TABLE issue_view VALIDATE CONSTRAINT issue_view_scope_type_check;
ALTER TABLE issue_view VALIDATE CONSTRAINT issue_view_check;
ALTER TABLE issue_view VALIDATE CONSTRAINT issue_view_scope_variant_pairing;
