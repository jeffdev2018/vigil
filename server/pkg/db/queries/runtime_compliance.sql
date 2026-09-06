-- Data residency routing (K46). What each runtime declares about where it
-- runs. See migration 701; the declaration is operator-supplied and unverified.

-- name: UpsertRuntimeComplianceProfile :one
-- The declaration is one row per runtime, so re-declaring overwrites. The
-- conflict target is the unique index from migration 702.
INSERT INTO runtime_compliance_profile (runtime_id, region, on_prem, declared_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (runtime_id) DO UPDATE
SET region = EXCLUDED.region,
    on_prem = EXCLUDED.on_prem,
    declared_by = EXCLUDED.declared_by,
    updated_at = now()
RETURNING *;

-- name: GetRuntimeComplianceProfile :one
SELECT * FROM runtime_compliance_profile WHERE runtime_id = $1;

-- name: DeleteRuntimeComplianceProfile :exec
DELETE FROM runtime_compliance_profile WHERE runtime_id = $1;

-- name: ListRuntimeComplianceProfiles :many
-- Batch read for a known candidate set: the runtime list response attaches one
-- declaration per row without a point query per runtime.
SELECT * FROM runtime_compliance_profile
WHERE runtime_id = ANY(@runtime_ids::uuid[]);

-- name: ListWorkspaceRuntimeComplianceProfiles :many
-- Every declaration in one workspace, in one round trip. This is what the
-- enqueue-time compliance filter loads: the policy is read once per enqueue
-- and so are the declarations, whatever the routing path then evaluates.
SELECT p.* FROM runtime_compliance_profile p
JOIN agent_runtime r ON r.id = p.runtime_id
WHERE r.workspace_id = $1;

-- name: PurgeWorkspaceRuntimeComplianceProfiles :exec
-- Workspace teardown: the profiles hang off the workspace's runtimes, so they
-- go before agent_runtime does.
DELETE FROM runtime_compliance_profile
WHERE runtime_id IN (SELECT id FROM agent_runtime WHERE workspace_id = $1);
