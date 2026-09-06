-- Code health autopilot (K22) opens maintenance issues stamped with
-- origin_type 'code_health' (origin_id = code_health_scan.id) so they are
-- recognisable as machine-made. NOT VALID keeps this an instant catalog
-- change; 698 validates separately so the scan does not inherit this lock.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run', 'code_health'))
    NOT VALID;
