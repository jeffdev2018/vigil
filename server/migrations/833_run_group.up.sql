-- A group of attempts racing on the same issue (F11 / JEF-6).
--
-- One row per "run three attempts and keep one". The attempts themselves are
-- ordinary agent_task_queue rows pointing back here through run_group_id: no
-- second scheduler, no shadow issue. A shadow issue per attempt was the
-- rejected alternative — it would have polluted board, inbox, counters and
-- dependencies with rows nobody asked for.
--
-- No foreign keys, per repository policy: issue_id, created_by and
-- winner_task_id are resolved and validated in application code.
--
-- status is TEXT with a CHECK rather than an enum so a later state costs a
-- migration, not a type rewrite. 'running' while attempts execute, 'settled'
-- once a winner is designated, 'abandoned' when the user drops the whole group.
--
-- attempt_count is written once at creation and never recomputed: it is the
-- number of attempts the user asked for, which is what the cap was checked
-- against, and it stays true even after losing tasks are cancelled.
CREATE TABLE IF NOT EXISTS run_group (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    created_by UUID,
    status TEXT NOT NULL DEFAULT 'running',
    winner_task_id UUID,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at TIMESTAMPTZ,
    CONSTRAINT run_group_status_check CHECK (status IN ('running', 'settled', 'abandoned'))
);

COMMENT ON TABLE run_group IS
    'A set of attempts racing on one issue (F11). Attempts are agent_task_queue rows carrying run_group_id.';
