-- Dated cycles (F29): a project's time-boxed iteration, with human and agent
-- capacity declared separately because the two are not fungible — a sprint
-- that fits its humans can still be impossible for its agents, and one number
-- hides that.
--
-- Overlapping cycles are ALLOWED: a project can run a support cycle beside a
-- feature cycle, and the rollover picks "the next cycle by start_date", which
-- stays well defined under overlap. No uniqueness on (project, dates).
--
-- load_property_id names a workspace number property (issue_property) whose
-- per-issue value is the load unit. NULL means the load unit is the issue
-- count, and the UI says so rather than pretending a story-point total.
--
-- No FOREIGN KEY, per the repository rule; the handler resolves and cleans up.
CREATE TABLE IF NOT EXISTS cycle (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    description TEXT NOT NULL DEFAULT '',
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    human_capacity INTEGER,
    agent_capacity INTEGER,
    load_property_id UUID,
    rollover BOOLEAN NOT NULL DEFAULT TRUE,
    closed_at TIMESTAMPTZ,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (start_date <= end_date)
);

COMMENT ON TABLE cycle IS
    'F29: a project''s dated iteration. Human and agent capacity are separate; load_property_id NULL means load = issue count. Overlaps allowed. No FK by house rule.';

-- One materialized row per open cycle per day, written by the
-- cycle_snapshot job. There is no status-history table, so a burndown cannot
-- be reconstructed after the fact — the snapshot IS the history.
--
-- workspace_id is denormalized (it is derivable through cycle_id) so workspace
-- teardown and the "every query filters by workspace_id" rule hold here too.
CREATE TABLE IF NOT EXISTS cycle_snapshot (
    cycle_id UUID NOT NULL,
    snapshot_date DATE NOT NULL,
    workspace_id UUID NOT NULL,
    total_count BIGINT NOT NULL DEFAULT 0,
    done_count BIGINT NOT NULL DEFAULT 0,
    total_load NUMERIC NOT NULL DEFAULT 0,
    done_load NUMERIC NOT NULL DEFAULT 0,
    human_load NUMERIC NOT NULL DEFAULT 0,
    agent_load NUMERIC NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cycle_id, snapshot_date)
);

COMMENT ON TABLE cycle_snapshot IS
    'F29: one daily row per open cycle. The only burndown history there is — status changes are not journaled in a queryable shape.';

ALTER TABLE issue ADD COLUMN IF NOT EXISTS cycle_id UUID;
