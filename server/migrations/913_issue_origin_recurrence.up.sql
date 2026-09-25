-- 'recurrence': an issue spawned by an issue_recurrence rule (origin_id = rule).
-- NOT VALID keeps this an instant catalog change; 914 validates separately.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run', 'code_health', 'linear', 'mirror', 'doc_drift', 'voice_mobile', 'epic', 'email', 'twenty', 'recurrence'))
    NOT VALID;
