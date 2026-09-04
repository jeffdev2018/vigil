-- Audit log (K08): an append-only record of the actions that matter —
-- decisions asked and answered, plans approved, status changes, proofs,
-- agent rollbacks, ownership rules, briefings, escalations, settings.
-- Every row is a denormalized snapshot at the time of the event so an
-- export stays stable when the source tables move on. No foreign keys by
-- house rule; purged only with the workspace, through a guarded path.
CREATE TABLE IF NOT EXISTS audit_log_entry (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL,
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_type     TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id       UUID,
    action         TEXT NOT NULL,
    entity_type    TEXT NOT NULL,
    entity_id      UUID,
    model          TEXT,
    cost_usd_ticks BIGINT,
    approver_type  TEXT,
    approver_id    UUID,
    details        JSONB NOT NULL DEFAULT '{}'
);
