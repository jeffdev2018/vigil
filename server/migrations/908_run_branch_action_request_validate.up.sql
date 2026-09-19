-- Validate the CHECKs 907 added NOT VALID. The table was introduced empty by
-- the same change, so no pre-existing row can fail the scan; VALIDATE runs
-- under SHARE UPDATE EXCLUSIVE and does not block concurrent reads or writes.
ALTER TABLE run_branch_action_request VALIDATE CONSTRAINT run_branch_action_request_action_check;
ALTER TABLE run_branch_action_request VALIDATE CONSTRAINT run_branch_action_request_status_check;
