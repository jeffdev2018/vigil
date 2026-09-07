-- Daemon execution memory (F24 / JEF-15): the durable notes a recurring
-- autopilot keeps for its own next run.
--
-- Deliberately NOT agent_memory. One agent can serve several autopilots, and
-- what a nightly triage job learned about its own queue is not something the
-- same agent should carry into an unrelated run. The scope is the recurring
-- job, so the row hangs off the autopilot and is briefed only to runs that
-- autopilot started.
--
-- One row per autopilot: this is a single document the agent rewrites, not a
-- list of facts, so the autopilot id IS the primary key and a write is an
-- upsert. `revision` is the optimistic-concurrency token the PUT matches with
-- If-Match, so two concurrent runs cannot silently clobber each other.
--
-- No FOREIGN KEY, per the repository rule; the handler deletes the row when
-- the autopilot is archived and workspace teardown purges by workspace_id.
CREATE TABLE IF NOT EXISTS autopilot_memory (
    autopilot_id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 0,
    updated_by_task_id UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE autopilot_memory IS
    'F24: one execution-memory document per autopilot, rewritten by its runs and re-injected into every following brief as DATA. revision is the If-Match token. No FK by house rule.';
