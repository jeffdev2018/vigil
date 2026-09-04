DELETE FROM agent_memory WHERE source = 'postmortem';
ALTER TABLE agent_memory DROP CONSTRAINT IF EXISTS agent_memory_source_check;
ALTER TABLE agent_memory ADD CONSTRAINT agent_memory_source_check
    CHECK (source IN ('manual', 'run'));
