-- Business rules (K53): a rule written in plain language, compiled once into
-- a structured predicate and evaluated deterministically at a fixed attach
-- point. Violations are kept for audit even after the rule is disabled.
CREATE TABLE business_rule (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    title TEXT NOT NULL,
    natural_language TEXT NOT NULL,
    compiled_predicate JSONB NOT NULL,
    attach_point TEXT NOT NULL CHECK (attach_point IN ('project_create', 'issue_submit_review', 'agent_run_dispatch')),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'disabled')),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE business_rule_violation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id UUID NOT NULL,
    detail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
