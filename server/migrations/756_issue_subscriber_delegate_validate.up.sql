-- Validate the widened issue_subscriber_reason_check 755 added NOT VALID.
-- 755 only WIDENED the allowed set, so no pre-existing row can fail the scan.
ALTER TABLE issue_subscriber VALIDATE CONSTRAINT issue_subscriber_reason_check;
