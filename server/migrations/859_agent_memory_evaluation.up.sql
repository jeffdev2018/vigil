CREATE TABLE agent_memory_evaluation (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    memory_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    memory_ids UUID[] NOT NULL,
    report JSONB NOT NULL,
    report_hash TEXT NOT NULL,
    uploaded_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    adopted_revision INTEGER
);
