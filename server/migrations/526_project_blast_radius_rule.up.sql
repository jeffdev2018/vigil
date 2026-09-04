-- Blast radius (K07): per-project autonomy by path pattern, layered over
-- the agent's permissions. The most specific pattern wins; two patterns of
-- equal specificity and different levels are refused at creation.
CREATE TABLE project_blast_radius_rule (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL,
    project_id     UUID NOT NULL,
    path_pattern   TEXT NOT NULL,
    autonomy_level TEXT NOT NULL CHECK (autonomy_level IN ('autonomous', 'read_only', 'dual_approval')),
    created_by     UUID NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
