-- JEF-246: per-actor declared capacity for a cycle ("an agent counts for X
-- velocity points"). The cycle-level human_capacity / agent_capacity totals
-- stay as the aggregate plan; this table is the per-person, per-agent
-- breakdown the velocity report measures done points against.
--
-- One row per (cycle, actor): actor_type 'member' carries a user id (the same
-- identity issue.assignee_id carries for members), 'agent' an agent id. The
-- primary key is attached by 921/922 through a CONCURRENTLY-built index, per
-- the repository rule that no index is built inside a multi-statement
-- migration.
--
-- No foreign keys and no cascades, per the repository rule: cycle deletion
-- removes these rows in the same transaction as the cycle, and workspace
-- teardown removes them in DeleteWorkspaceRuntimesAndProjects before the
-- cycle rows go.
CREATE TABLE IF NOT EXISTS cycle_actor_capacity (
    id           UUID NOT NULL DEFAULT gen_random_uuid(),
    cycle_id     UUID NOT NULL,
    workspace_id UUID NOT NULL,
    actor_type   TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id     UUID NOT NULL,
    points       INTEGER NOT NULL CHECK (points >= 0 AND points <= 10000),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE cycle_actor_capacity IS
    'JEF-246: per-actor declared velocity capacity for one cycle. Row present = declared; no row means undeclared, which is different from zero.';
COMMENT ON COLUMN cycle_actor_capacity.actor_id IS
    'user id when actor_type = ''member'' (matching issue.assignee_id), agent id when actor_type = ''agent''.';
