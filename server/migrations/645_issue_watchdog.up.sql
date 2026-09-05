-- Task watchdog (K73): an optional agent, different from the assignee, that
-- inspects an issue subtree once it is at rest and returns a verdict. No FKs
-- by repo rule; the handler resolves and cleans up.
CREATE TABLE IF NOT EXISTS issue_watchdog (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    instructions TEXT NOT NULL DEFAULT '',
    rest_minutes INTEGER NOT NULL DEFAULT 30,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_scan_task_id UUID,
    last_scanned_at TIMESTAMPTZ,
    motion_streak INTEGER NOT NULL DEFAULT 0,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id)
);

CREATE TABLE IF NOT EXISTS watchdog_verdict (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    watchdog_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    task_id UUID NOT NULL,
    verdict TEXT NOT NULL CHECK (verdict IN ('legitimate', 'motion', 'escalate')),
    summary TEXT NOT NULL DEFAULT '',
    findings JSONB NOT NULL DEFAULT '[]'::jsonb,
    dropped JSONB NOT NULL DEFAULT '[]'::jsonb,
    applied JSONB NOT NULL DEFAULT '{}'::jsonb,
    decision_id UUID,
    human_review TEXT NOT NULL DEFAULT 'pending' CHECK (human_review IN ('pending', 'confirmed', 'overturned')),
    contract_revision INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Outcome Contract (K12) minimal extension: a risk level and a revision the
-- watchdog can cite; a criteria write bumps the revision (supersedes = revision - 1).
ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS contract_risk TEXT NOT NULL DEFAULT 'normal' CHECK (contract_risk IN ('low', 'normal', 'high')),
    ADD COLUMN IF NOT EXISTS contract_revision INTEGER NOT NULL DEFAULT 0;
