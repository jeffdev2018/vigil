-- Approving a postmortem copies its preventive rules into the failed agent's
-- memory so the next run sees them. Those rows carry their own source value.
ALTER TABLE agent_memory DROP CONSTRAINT IF EXISTS agent_memory_source_check;
ALTER TABLE agent_memory ADD CONSTRAINT agent_memory_source_check
    CHECK (source IN ('manual', 'run', 'postmortem'));
