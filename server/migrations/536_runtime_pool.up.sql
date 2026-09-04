-- Runtime pools (K28): an agent may target an ordered family of
-- interchangeable runtimes instead of one. When its runtime is offline at
-- enqueue, stays offline while a task waits, or a run fails for an
-- infrastructure reason, the task moves to the next online runtime of the
-- pool; the degraded runtime (a local model) is the explicit last resort.
-- Every move is recorded on the task so it is never silent.
CREATE TABLE runtime_pool (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL,
    name                TEXT NOT NULL,
    runtime_ids         JSONB NOT NULL DEFAULT '[]'::jsonb,
    degraded_runtime_id UUID,
    created_by          UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE agent ADD COLUMN IF NOT EXISTS runtime_pool_id UUID;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS failover_history JSONB;
