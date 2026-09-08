-- Epic Mode (F18) stamps two kinds of issue with origin_type 'epic': the host
-- issue the generation runs are enqueued on (origin_id = the project), and
-- every child ticket the tickets step applies (origin_id = the project too).
-- Both exist because a human approved an epic step, so neither is an
-- autopilot run and neither points at a task.
-- NOT VALID keeps this an instant catalog change; 765 validates separately.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run', 'code_health', 'linear', 'mirror', 'doc_drift', 'voice_mobile', 'epic'))
    NOT VALID;
