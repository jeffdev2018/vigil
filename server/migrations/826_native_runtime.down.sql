-- Reverse 826: drop the seeded native runtimes, then tighten the CHECK back.
-- Agents still bound to a native runtime keep their runtime_id dangling; the
-- claim fence (agent.runtime_id = task.runtime_id) simply never matches, the
-- same degraded state as any deleted runtime row.
DELETE FROM agent_runtime WHERE runtime_mode = 'native' AND daemon_id = 'native';

ALTER TABLE agent_runtime DROP CONSTRAINT IF EXISTS agent_runtime_runtime_mode_check;
ALTER TABLE agent_runtime ADD CONSTRAINT agent_runtime_runtime_mode_check
    CHECK (runtime_mode IN ('local', 'cloud'));
