-- Fan-out / fan-in (K38): a leader launches N specialist runs (one child
-- issue each, assigned to a squad member) and a barrier starts the leader's
-- synthesis run once every child finished or failed for good. Each child
-- run works as usual; the batch is a grouping layer above it.
CREATE TABLE fanout_batch (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    parent_issue_id   UUID NOT NULL,
    leader_agent_id   UUID NOT NULL,
    expected_count    INTEGER NOT NULL CHECK (expected_count > 0),
    completed_count   INTEGER NOT NULL DEFAULT 0 CHECK (completed_count >= 0),
    failed_count      INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'partial_failure', 'complete')),
    synthesis_task_id UUID,
    started_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at      TIMESTAMPTZ
);
CREATE TABLE fanout_batch_member (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fanout_batch_id      UUID NOT NULL,
    workspace_id         UUID NOT NULL,
    child_issue_id       UUID NOT NULL,
    task_id              UUID NOT NULL,
    assignee_agent_id    UUID NOT NULL,
    sub_task_description TEXT NOT NULL,
    outcome              TEXT CHECK (outcome IS NULL OR outcome IN ('completed', 'failed')),
    settled_task_id      UUID,
    settled_at           TIMESTAMPTZ
);
