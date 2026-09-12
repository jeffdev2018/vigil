-- Native onboarding (OS plan, chantier 5).

-- name: SeedNativeRuntimeForWorkspace :execrows
-- The per-workspace native runtime row, created with the workspace instead of
-- waiting for the next server tick (same shape as SeedNativeRuntimes).
INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, visibility, owner_id)
SELECT sqlc.arg('workspace_id')::uuid, 'native', 'Native runtime', 'native', 'native', 'online', 'in-server agent runtime', 'public', sqlc.arg('owner_id')::uuid
WHERE NOT EXISTS (
    SELECT 1 FROM agent_runtime r
    WHERE r.workspace_id = sqlc.arg('workspace_id')::uuid AND r.runtime_mode = 'native'
);

-- name: GetOnboardingChecklistCounts :one
-- The five signals a fresh workspace's checklist reads, in one round trip.
SELECT
    (SELECT COUNT(*) FROM agent_runtime r WHERE r.workspace_id = $1 AND r.runtime_mode = 'native')::bigint AS native_runtimes,
    (SELECT COUNT(*) FROM agent_runtime r WHERE r.workspace_id = $1 AND r.runtime_mode <> 'native' AND r.status = 'online')::bigint AS online_daemon_runtimes,
    (SELECT COUNT(*) FROM agent a WHERE a.workspace_id = $1 AND a.archived_at IS NULL)::bigint AS agents,
    (SELECT COUNT(*) FROM issue i WHERE i.workspace_id = $1)::bigint AS issues,
    (SELECT COUNT(*) FROM agent_task_queue atq JOIN agent a ON a.id = atq.agent_id WHERE a.workspace_id = $1 AND atq.status = 'completed')::bigint AS completed_runs,
    (SELECT COUNT(*) FROM issue_decision d WHERE d.workspace_id = $1 AND d.response IS NOT NULL)::bigint AS answered_decisions;
