-- Eval Lab (K24): a proven issue becomes a reusable evaluation case (its
-- acceptance criteria and their reference proofs, snapshotted); a suite groups
-- cases; a run replays every case of a suite against one pinned agent version
-- in a throwaway sandboxed issue and scores it on how many criteria the agent
-- managed to prove again. Nothing here touches the source issue.
CREATE TABLE eval_case (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       UUID NOT NULL,
    source_issue_id    UUID NOT NULL,
    source_issue_number INT NOT NULL DEFAULT 0,
    title              TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    criteria           JSONB NOT NULL DEFAULT '[]',
    created_by         UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE eval_suite (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name         TEXT NOT NULL,
    case_ids     JSONB NOT NULL DEFAULT '[]',
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE eval_run (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    suite_id         UUID NOT NULL,
    agent_id         UUID NOT NULL,
    agent_version_id UUID,
    status           TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed')),
    score            INT,
    started_by       UUID,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);

CREATE TABLE eval_run_case (
    run_id     UUID NOT NULL,
    case_id    UUID NOT NULL,
    issue_id   UUID NOT NULL,
    task_id    UUID NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'passed', 'failed', 'infra_failed')),
    score      INT,
    detail     TEXT NOT NULL DEFAULT '',
    settled_at TIMESTAMPTZ,
    PRIMARY KEY (run_id, case_id)
);
