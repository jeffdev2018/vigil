-- Native runtime (rowboat borrow, lot A): agents executed in-memory by the
-- Go server through the internal LLM layer, instead of a CLI on a daemon.
-- runtime_mode grows a third value; the CHECK was created inline in
-- migration 004 under PostgreSQL's canonical name.
ALTER TABLE agent_runtime DROP CONSTRAINT IF EXISTS agent_runtime_runtime_mode_check;
ALTER TABLE agent_runtime ADD CONSTRAINT agent_runtime_runtime_mode_check
    CHECK (runtime_mode IN ('local', 'cloud', 'native'));

-- One native runtime per existing workspace. daemon_id 'native' + provider
-- 'native' ride the existing UNIQUE (workspace_id, daemon_id, provider), so
-- the pair is the per-workspace singleton key. The server's tick keeps
-- last_seen_at fresh for new workspaces the same way (SeedNativeRuntimes).
INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info)
SELECT w.id, 'native', 'Native runtime', 'native', 'native', 'online', 'in-server agent runtime'
FROM workspace w
WHERE NOT EXISTS (
    SELECT 1 FROM agent_runtime r
    WHERE r.workspace_id = w.id AND r.runtime_mode = 'native'
);
