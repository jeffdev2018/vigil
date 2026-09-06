-- Voice-dictated issue draft (K36) stamps the issues it creates with
-- origin_type 'voice_mobile', so a draft that started as a phone dictation
-- stays distinguishable from one typed into the ordinary create form.
-- Unlike every other origin here it carries no origin_id: a transcript is
-- not a stored row, so there is nothing to point at.
-- NOT VALID keeps this an instant catalog change; 739 validates separately.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run', 'code_health', 'linear', 'mirror', 'doc_drift', 'voice_mobile'))
    NOT VALID;
