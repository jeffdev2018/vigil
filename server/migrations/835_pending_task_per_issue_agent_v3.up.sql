-- The pending-slot uniqueness, made blind to grouped attempts (F11 / JEF-6).
--
-- v2 enforces one pending row per (issue, agent) over the whole queue. That is
-- the rule two attempts of the same agent on one issue have to break — a race
-- whose whole point is "the same agent, two models" cannot insert its second
-- attempt while v2 stands. It is the INSERT-time half of the same serialization
-- the claim enforces at dispatch time; relaxing one without the other would
-- have made the feature fail earlier, not differently.
--
-- v3 is v2 plus `run_group_id IS NULL`. For every row that exists today and for
-- every run enqueued outside a group, run_group_id IS NULL, the added predicate
-- is TRUE, and the index is byte-for-byte the rule v2 enforced. The relaxation
-- is exactly the set of rows the feature creates and nothing else; what bounds
-- them is the group's own attempt cap, checked before the fan-out.
--
-- Built before v2 is dropped (migration 836) so the uniqueness rule holds
-- through a rolling deploy and through an interrupted migration run: v3's
-- predicate is a subset of v2's, so both coexist safely.
--
-- Single statement, CONCURRENTLY, per repository policy.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_one_pending_task_per_issue_agent_v3
    ON agent_task_queue (issue_id, agent_id)
    WHERE run_group_id IS NULL
      AND (status IN ('queued', 'dispatched')
           OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true'));
