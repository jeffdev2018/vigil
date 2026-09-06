-- Cross-repo mirror issues (K54) stamp each generated mirror with origin_type
-- 'mirror' (origin_id = the source issue) so it is recognisable as generated
-- and never mistaken for hand-written work. NOT VALID keeps this an instant
-- catalog change; 726 validates separately.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run', 'code_health', 'linear', 'mirror'))
    NOT VALID;
