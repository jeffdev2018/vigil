-- Per-agent durable memory (JEF-236): facts an agent accumulated from past
-- runs ("this repo uses pnpm, never npm"), injected into every claimed task's
-- brief and editable over REST. source distinguishes human-authored rows
-- ('manual', never auto-evicted) from rows the post-run extraction pass wrote
-- ('run', evicted oldest-first once the agent exceeds the 200-fact cap).
-- source_task_id records the run a 'run' fact was extracted from.
--
-- No foreign keys or cascades by repo rule: agent/workspace cleanup is
-- application-side (builder-carrier delete and the workspace delete sweep).
-- The primary key is attached by 464 from the CONCURRENTLY-built index in 463.
CREATE TABLE agent_memory (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    content TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'run')),
    source_task_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (btrim(content) <> ''),
    CHECK (length(content) <= 500)
);
