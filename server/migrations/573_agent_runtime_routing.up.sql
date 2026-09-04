-- Runtime routing mode for agents (JEF-237). 'fixed' (default) preserves the
-- current behavior: the agent always runs on its bound runtime with its
-- configured model. 'auto' lets the server pick the most promising
-- (runtime, model) pair per enqueued task from historical run statistics.
-- The bound runtime_id / model stay in place as the routing fallback.
-- The CHECK rides the ADD COLUMN so IF NOT EXISTS skips both on a re-run.
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS runtime_routing TEXT NOT NULL DEFAULT 'fixed'
        CHECK (runtime_routing IN ('fixed', 'auto'));
