-- Agent context drift detection (K56) hangs its scan and pull-request runs off
-- one housekeeping issue stamped with origin_type 'doc_drift', so the issue is
-- recognisable as machine-made and findable without matching on its title.
-- NOT VALID keeps this an instant catalog change; 734 validates separately.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run', 'code_health', 'linear', 'mirror', 'doc_drift'))
    NOT VALID;
