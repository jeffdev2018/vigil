-- Agent duel (K39): two agents run the same issue independently at the
-- same time; an arbiter scores both runs (quality, trajectory, cost,
-- duration) and a human confirms the winner. Each run is an ordinary run;
-- the duel is a grouping layer above them. Nothing here changes the issue.
CREATE TABLE agent_duel (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    issue_id        UUID NOT NULL,
    agent_a_id      UUID NOT NULL,
    agent_b_id      UUID NOT NULL,
    task_a_id       UUID NOT NULL,
    task_b_id       UUID NOT NULL,
    final_task_a_id UUID,
    final_task_b_id UUID,
    outcome_a       TEXT CHECK (outcome_a IS NULL OR outcome_a IN ('completed', 'failed')),
    outcome_b       TEXT CHECK (outcome_b IS NULL OR outcome_b IN ('completed', 'failed')),
    status          TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'verdict_ready', 'confirmed', 'inconclusive')),
    verdict         JSONB,
    arbiter_error   TEXT,
    winner          TEXT CHECK (winner IS NULL OR winner IN ('a', 'b', 'tie')),
    started_by      UUID,
    confirmed_by    UUID,
    confirmed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at      TIMESTAMPTZ
);
