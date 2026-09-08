-- name: SeedNativeRuntimes :execrows
-- Idempotent per-workspace singleton row for the native runtime. Runs from the
-- server tick so a workspace created after migration 826 still gets its row
-- without hooking workspace creation. Mirrors the migration's INSERT exactly.
INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info)
SELECT w.id, 'native', 'Native runtime', 'native', 'native', 'online', 'in-server agent runtime'
FROM workspace w
WHERE NOT EXISTS (
    SELECT 1 FROM agent_runtime r
    WHERE r.workspace_id = w.id AND r.runtime_mode = 'native'
);

-- name: HeartbeatNativeRuntimes :execrows
-- ClaimAgentTask requires status='online' and a last_seen_at fresher than the
-- claim freshness window, so the server tick keeps its own runtimes eligible
-- exactly the way a daemon's poll does.
UPDATE agent_runtime
SET last_seen_at = now(), status = 'online', updated_at = now()
WHERE runtime_mode = 'native' AND daemon_id = 'native';

-- name: ListNativeRuntimes :many
SELECT * FROM agent_runtime
WHERE runtime_mode = 'native' AND daemon_id = 'native'
ORDER BY created_at;
