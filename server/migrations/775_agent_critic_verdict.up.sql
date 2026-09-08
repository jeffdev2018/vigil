-- F25: one critic's answer about one delivery.
--
-- The row is written when the verdict lands, never when the critic run is
-- enqueued: a critic run that dies without answering must read as "no verdict"
-- rather than as a pending one that something has to time out. The link back
-- to the run that was supposed to answer is critic_task_id, and the run itself
-- carries the pointer the other way in its context stamp (critic_of_task_id),
-- which is what lets the completion hook recognise a critic run at all.
--
-- verdict is the whole product decision, in three values:
--   pass     — nothing in the way; the delivery finalises as it would have.
--   concerns — worth reading, not worth another round. Never blocks.
--   block    — the author is relaunched with summary + findings as its brief.
-- reason carries WHY when the platform wrote the verdict itself rather than
-- the critic: no_distinct_provider, max_rounds, max_cost, no_verdict.
--
-- findings shares the F06 review-flag vocabulary (severity / file / line /
-- title / note) so a reader who knows one knows the other; it is JSONB rather
-- than a child table because nothing queries inside it.
--
-- No FOREIGN KEY, per the repository rule.
CREATE TABLE IF NOT EXISTS agent_critic_verdict (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    issue_id         UUID NOT NULL,
    subject_task_id  UUID NOT NULL,
    critic_task_id   UUID,
    phase            TEXT NOT NULL DEFAULT 'change',
    verdict          TEXT NOT NULL CHECK (verdict IN ('pass', 'concerns', 'block')),
    reason           TEXT,
    summary          TEXT,
    findings         JSONB NOT NULL DEFAULT '[]',
    round            INT NOT NULL DEFAULT 1,
    cost_usd_ticks   BIGINT NOT NULL DEFAULT 0,
    created_by_type  TEXT,
    created_by_id    UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE agent_critic_verdict IS
    'F25: one structured critic verdict (pass|concerns|block) on one delivery. No FK by house rule.';
