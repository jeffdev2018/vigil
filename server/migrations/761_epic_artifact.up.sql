-- Epic Mode (F18 / JEF-30): a project-level artifact pipeline — PRD, tech
-- plan, wireframe, tickets — with a human gate between the steps.
--
-- Shaped after issue_plan (462): every publication is a new row with the next
-- version for its kind, and the previous rows of that kind become
-- 'superseded'. No FK by house rule; cleanup lives in workspace_delete.
--
-- A row with an empty `content` and a `generated_by_task_id` is a GENERATION
-- CLAIM: the version an in-flight agent run reserved. It is what the API
-- reports as `generating`, and the (project_id, kind, version) unique index
-- of 762 is the fence that keeps two concurrent generates from claiming the
-- same version. The settle hook refuses an empty block, so a settled row
-- always has content — which is what makes the emptiness a reliable
-- discriminator rather than a guess.
CREATE TABLE IF NOT EXISTS epic_artifact (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL,
    project_id           UUID NOT NULL,
    kind                 TEXT NOT NULL CHECK (kind IN ('prd', 'tech_plan', 'wireframe', 'tickets')),
    version              INTEGER NOT NULL CHECK (version > 0),
    content              TEXT NOT NULL DEFAULT '',
    -- Structured side of the artifact as the author wrote it. For `tickets`
    -- it carries {tickets: [...], applied: {external_key: issue_id}}.
    payload              JSONB NOT NULL DEFAULT '{}'::jsonb,
    state                TEXT NOT NULL CHECK (state IN ('draft', 'approved', 'superseded')),
    -- The host issue the generation runs are enqueued on, created on the
    -- first generate and copied onto every later row of the project.
    epic_issue_id        UUID,
    generated_by_task_id UUID,
    approved_by          UUID,
    approved_at          TIMESTAMPTZ,
    author_type          TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id            UUID NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE epic_artifact IS
    'Versioned project artifact for Epic Mode (F18): prd -> tech_plan -> wireframe -> tickets, one row per publication. A row with empty content and a generated_by_task_id is an in-flight generation claim. No FK by house rule.';
