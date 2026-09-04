-- Sharded refactoring campaigns with a merge queue (K42). A campaign is a
-- fan-out (K38) whose sub-tasks each work on their own branch; once a
-- shard's branch is merge-ready (F10) it enters the campaign's queue, which
-- merges one shard at a time in position order (rebase run, then merge).
CREATE TABLE refactor_campaign (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    issue_id        UUID NOT NULL,
    fanout_batch_id UUID NOT NULL,
    name            TEXT NOT NULL,
    target_branch   TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'merging', 'completed', 'failed')),
    started_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);
CREATE TABLE campaign_shard (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    refactor_campaign_id UUID NOT NULL,
    workspace_id         UUID NOT NULL,
    fanout_member_id     UUID NOT NULL,
    child_issue_id       UUID NOT NULL,
    task_id              UUID NOT NULL,
    assignee_agent_id    UUID NOT NULL,
    description          TEXT NOT NULL,
    branch_name          TEXT NOT NULL,
    merge_position       INTEGER NOT NULL CHECK (merge_position >= 0),
    merge_status         TEXT NOT NULL DEFAULT 'pending' CHECK (merge_status IN ('pending', 'rebasing', 'ready', 'merged', 'conflict', 'skipped')),
    merge_task_id        UUID,
    blockers             JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
