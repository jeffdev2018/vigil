-- Issues accepted from the triage queue keep their item's origin. Two origins
-- reach the queue without being in this list: 'email' (610, inbound email
-- intake) and 'twenty' (Twenty CRM webhooks, OS plan chantier 2). Both are
-- added here so accepting such an item no longer trips the CHECK.
-- NOT VALID keeps this an instant catalog change; 872 validates separately.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run', 'code_health', 'linear', 'mirror', 'doc_drift', 'voice_mobile', 'epic', 'email', 'twenty'))
    NOT VALID;
