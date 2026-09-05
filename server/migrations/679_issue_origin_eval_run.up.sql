-- Eval Lab (K24) creates a throwaway replay issue per case, stamped with
-- origin_type 'eval_run' (origin_id = eval_run.id) so it is recognisable as
-- machine-made and never mistaken for real work. NOT VALID keeps this an
-- instant catalog change; 680 validates separately so the scan does not
-- inherit this lock.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'meeting', 'eval_run'))
    NOT VALID;
