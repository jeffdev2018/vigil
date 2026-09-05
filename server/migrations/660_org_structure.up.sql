-- Executable org chart (K75): one structure per project, or the workspace
-- default, in one of seven models. Units, roles, memberships, edges and
-- routing rules live in the validated JSON definition; every save writes a
-- revision so a run can name the structure in force. No foreign keys per
-- the migration rules; the handler resolves and cleans up.
CREATE TABLE IF NOT EXISTS org_structure (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    project_id UUID,
    model TEXT NOT NULL CHECK (model IN ('hierarchy', 'squads', 'matrix', 'circles', 'owner_network', 'taskforce', 'market')),
    name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'paused', 'dissolved')),
    revision INTEGER NOT NULL DEFAULT 1,
    revision_id UUID,
    definition JSONB NOT NULL DEFAULT '{}'::jsonb,
    owner_id UUID,
    dissolve_at TIMESTAMPTZ,
    end_condition TEXT NOT NULL DEFAULT '',
    budget_usd_ticks BIGINT NOT NULL DEFAULT 0,
    eval_attestation TEXT NOT NULL DEFAULT '',
    paused_reason TEXT NOT NULL DEFAULT '',
    dissolved_at TIMESTAMPTZ,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS org_revision (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    structure_id UUID NOT NULL,
    revision INTEGER NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL,
    definition JSONB NOT NULL,
    changed_by UUID,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Internal market: an offer an agent makes on an issue.
CREATE TABLE IF NOT EXISTS org_offer (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    structure_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    cost_usd_ticks BIGINT NOT NULL DEFAULT 0,
    eta_hours DOUBLE PRECISION NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'won', 'lost', 'over_cap')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Measured flows (routing, escalation, offers, breaker) the living org reads.
CREATE TABLE IF NOT EXISTS org_flow (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    structure_id UUID NOT NULL,
    unit_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    issue_id UUID,
    actor_type TEXT NOT NULL DEFAULT '',
    actor_id UUID,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
