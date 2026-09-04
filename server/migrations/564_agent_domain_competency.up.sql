-- Learned competency routing (K43): a rolling success tally per agent and
-- domain (a label, or the code area the issue touched), fed at issue
-- closure and by confirmed duel verdicts (K39), kept separately so the
-- weight of a duel stays traceable. Per workspace: a shared agent's
-- history never mixes across workspaces.
CREATE TABLE agent_domain_competency (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    agent_id      UUID NOT NULL,
    domain_key    TEXT NOT NULL,
    success_count INTEGER NOT NULL DEFAULT 0 CHECK (success_count >= 0),
    total_count   INTEGER NOT NULL DEFAULT 0 CHECK (total_count >= 0),
    duel_wins     INTEGER NOT NULL DEFAULT 0 CHECK (duel_wins >= 0),
    duel_losses   INTEGER NOT NULL DEFAULT 0 CHECK (duel_losses >= 0),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
