-- CI auto-fix (K49): one row per correction run launched because the CI
-- of an agent's pull request went red. The (pull request, head sha) pair is
-- unique: one attempt per failing head, the count per pull request is the
-- attempts cap. task_id is filled once the run is queued. No foreign key.
CREATE TABLE ci_auto_fix_run (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    provider         TEXT NOT NULL CHECK (provider IN ('github', 'vcs')),
    pull_request_id  UUID NOT NULL,
    head_sha         TEXT NOT NULL,
    issue_id         UUID NOT NULL,
    task_id          UUID,
    source_task_id   UUID,
    attempt          INTEGER NOT NULL CHECK (attempt >= 1),
    budget_usd_ticks BIGINT NOT NULL DEFAULT 0,
    manual           BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
