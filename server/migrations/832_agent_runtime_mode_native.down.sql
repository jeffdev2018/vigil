-- Reverse 832: tighten the agent.runtime_mode CHECK back. Rows already
-- carrying 'native' would block the ADD until they are rebound or removed.
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_runtime_mode_check;
ALTER TABLE agent ADD CONSTRAINT agent_runtime_mode_check
    CHECK (runtime_mode IN ('local', 'cloud'));
