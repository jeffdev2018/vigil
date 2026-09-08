-- Per-leg accounting (JEF-274). A multi-leg workflow (draft, review, revision,
-- retry, fallback, escalation, ...) is a set of separate agent_task_queue rows,
-- each with its own task_usage. These two columns make that set addressable:
--
--   leg_role              what this run is in its workflow. Empty means the
--                         primary (draft/single) leg — the run the workflow
--                         starts from. Free TEXT so a new producer needs no
--                         migration; service.LegRole* holds the known values.
--   workflow_root_task_id the primary leg every leg of the workflow points at.
--                         NULL on the root itself, so a workflow is
--                         `id = root OR workflow_root_task_id = root`.
--
-- No FK, per repo convention: the link is resolved in the app layer.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS leg_role TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS workflow_root_task_id UUID;
