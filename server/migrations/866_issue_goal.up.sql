-- Goal loop (long tasks): the goal an issue is worked toward and the state of
-- the run chain that pursues it. One row per issue, created on the first
-- judged run or when a member writes the goal. No foreign keys (house rule);
-- issue and run references are resolved in the application layer.
--
-- status: active (runs may be queued), paused (a member stopped the chain),
-- waiting_user (an agent asked a typed question, nothing runs until the
-- answer), satisfied (the judge found the goal met), stopped (the chain ended
-- short of the goal: exhausted, stagnation, external wait, failed run).
CREATE TABLE issue_goal (
    id                UUID PRIMARY KEY,
    workspace_id      UUID NOT NULL,
    issue_id          UUID NOT NULL,
    goal              TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused', 'waiting_user', 'satisfied', 'stopped')),
    continuation      INTEGER NOT NULL DEFAULT 0,
    max_continuations INTEGER NOT NULL DEFAULT 8,
    no_progress       INTEGER NOT NULL DEFAULT 0,
    last_signature    TEXT NOT NULL DEFAULT '',
    last_outcome      TEXT NOT NULL DEFAULT '',
    last_blocker      TEXT NOT NULL DEFAULT '',
    last_reason       TEXT NOT NULL DEFAULT '',
    next_step         TEXT NOT NULL DEFAULT '',
    evidence          JSONB NOT NULL DEFAULT '[]'::jsonb,
    question          JSONB,
    last_run_id       UUID,
    done_request_id   UUID,
    set_by_type       TEXT NOT NULL DEFAULT 'system'
        CHECK (set_by_type IN ('member', 'agent', 'system')),
    set_by_id         UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
