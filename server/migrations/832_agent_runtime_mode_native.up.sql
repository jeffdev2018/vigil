-- Follow-up to 826 (second gap found by the deployed smoke): the agent TABLE
-- carries its own runtime_mode copy with its own CHECK, still ('local',
-- 'cloud'). CreateAgent stamps the bound runtime's mode onto the agent row,
-- so binding an agent to a native runtime failed the constraint with a 500.
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_runtime_mode_check;
ALTER TABLE agent ADD CONSTRAINT agent_runtime_mode_check
    CHECK (runtime_mode IN ('local', 'cloud', 'native'));
