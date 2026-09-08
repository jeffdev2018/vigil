-- Code health autopilot (K22). A scheduled read-only agent run inspects the
-- project's repository and reports maintenance opportunities — debt, outdated
-- or vulnerable dependencies, missing tests. One row per scan: the run that
-- produced it, the findings it reported, and how many issues were opened from
-- them. Disabled by default; the configuration lives under
-- workspace.settings.code_health.
--
-- No foreign keys by repository convention: workspace teardown deletes these
-- rows explicitly (purge step in handler/workspace.go).
CREATE TABLE IF NOT EXISTS code_health_scan (
    id             UUID PRIMARY KEY,
    workspace_id   UUID NOT NULL,
    -- Optional project scope; NULL means the whole workspace.
    project_id     UUID,
    agent_id       UUID NOT NULL,
    -- The read-only agent run whose fenced output this scan is waiting on.
    task_id        UUID NOT NULL,
    status         TEXT NOT NULL DEFAULT 'running'
                   CHECK (status IN ('running', 'completed', 'failed', 'empty')),
    -- The findings kept after filtering, each carrying the id of the issue it
    -- opened (or the reason it was skipped).
    findings       JSONB NOT NULL DEFAULT '[]'::jsonb,
    issues_created INT NOT NULL DEFAULT 0,
    error          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at   TIMESTAMPTZ
);
