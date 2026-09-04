-- Trust Dial (K26): how far an agent may act before it must stop and ask.
-- Orthogonal to permissions. New agents start in propose; existing agents
-- keep today's behaviour (approval) so nothing changes under them.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS trust_mode TEXT NOT NULL DEFAULT 'propose'
    CHECK (trust_mode IN ('observer', 'propose', 'approval', 'autonomous'));
UPDATE agent SET trust_mode = 'approval';

CREATE TABLE trust_mode_change (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    agent_id          UUID NOT NULL,
    from_mode         TEXT NOT NULL,
    to_mode           TEXT NOT NULL,
    reason            TEXT,
    triggered_by_type TEXT NOT NULL CHECK (triggered_by_type IN ('member', 'system_suggested')),
    triggered_by_id   UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
