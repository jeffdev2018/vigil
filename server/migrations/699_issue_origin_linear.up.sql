-- Linear Bridge (K21) mirrors a Linear issue into a Multica issue stamped
-- origin_type 'linear' (origin_id = linear_installation.id). NOT VALID keeps
-- this an instant catalog change; 700 validates separately.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run', 'code_health', 'linear'))
    NOT VALID;
