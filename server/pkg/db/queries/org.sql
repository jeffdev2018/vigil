-- Executable org chart (K75).

-- name: ListOrgStructures :many
SELECT * FROM org_structure WHERE workspace_id = $1 ORDER BY (project_id IS NULL) DESC, created_at ASC;

-- name: GetOrgStructure :one
SELECT * FROM org_structure WHERE id = $1 AND workspace_id = $2;

-- name: GetOrgStructureForProject :one
SELECT * FROM org_structure WHERE workspace_id = $1 AND project_id = $2 AND status <> 'dissolved';

-- name: GetOrgStructureDefault :one
SELECT * FROM org_structure WHERE workspace_id = $1 AND project_id IS NULL AND status <> 'dissolved';

-- name: ListLiveOrgStructures :many
SELECT * FROM org_structure WHERE status IN ('active', 'paused') ORDER BY created_at ASC;

-- name: CreateOrgStructure :one
INSERT INTO org_structure (id, workspace_id, project_id, model, name, status, revision, revision_id, definition, owner_id, dissolve_at, end_condition, budget_usd_ticks, eval_attestation, created_by)
VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: UpdateOrgStructure :one
UPDATE org_structure SET
    model = $3, name = $4, status = $5, revision = revision + 1, revision_id = $6, definition = $7, owner_id = $8,
    dissolve_at = $9, end_condition = $10, budget_usd_ticks = $11, eval_attestation = $12, paused_reason = '', updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SetOrgStructureStatus :one
UPDATE org_structure SET status = $3, paused_reason = $4, dissolved_at = CASE WHEN $3 = 'dissolved' THEN now() ELSE dissolved_at END, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteOrgStructure :exec
DELETE FROM org_structure WHERE id = $1 AND workspace_id = $2;

-- name: CreateOrgRevision :one
INSERT INTO org_revision (id, workspace_id, structure_id, revision, model, status, definition, changed_by, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListOrgRevisions :many
SELECT * FROM org_revision WHERE structure_id = $1 AND workspace_id = $2 ORDER BY revision DESC LIMIT 50;

-- name: GetOrgRevision :one
SELECT * FROM org_revision WHERE id = $1 AND workspace_id = $2;

-- name: CreateOrgOffer :one
INSERT INTO org_offer (id, workspace_id, structure_id, issue_id, agent_id, confidence, cost_usd_ticks, eta_hours, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListOrgOffersForIssue :many
SELECT o.*, a.name AS agent_name FROM org_offer o JOIN agent a ON a.id = o.agent_id
WHERE o.issue_id = $1 AND o.workspace_id = $2 ORDER BY o.created_at ASC;

-- name: CountAgentOffersSince :one
SELECT count(*) FROM org_offer WHERE agent_id = $1 AND created_at >= sqlc.arg('since')::timestamptz;

-- name: CreateOrgFlow :exec
INSERT INTO org_flow (id, workspace_id, structure_id, unit_id, kind, issue_id, actor_type, actor_id, details)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CountOrgFlowsSince :one
SELECT count(*) FROM org_flow
WHERE structure_id = $1 AND kind = $2 AND created_at >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('unit_id')::text IS NULL OR unit_id = sqlc.narg('unit_id')::text);

-- name: ListOrgFlowsSince :many
SELECT * FROM org_flow WHERE structure_id = $1 AND created_at >= sqlc.arg('since')::timestamptz ORDER BY created_at DESC LIMIT 2000;

-- name: CountIssuesEscalatedTwiceSince :one
-- Stacked escalations: issues escalated at least twice in the window.
SELECT count(*) FROM (
    SELECT issue_id FROM org_flow
    WHERE structure_id = $1 AND kind = 'escalation' AND created_at >= sqlc.arg('since')::timestamptz AND issue_id IS NOT NULL
    GROUP BY issue_id HAVING count(*) >= 2
) s;

-- name: SumOrgUnitSpendSince :one
-- What a unit's routed issues cost: usage of the runs on issues the unit was routed.
SELECT COALESCE(SUM(u.cost_usd_ticks), 0)::bigint FROM task_usage u
JOIN agent_task_queue q ON q.id = u.task_id
WHERE q.issue_id IN (
    SELECT DISTINCT f.issue_id FROM org_flow f
    WHERE f.structure_id = $1 AND f.unit_id = $2 AND f.kind = 'routing' AND f.issue_id IS NOT NULL
) AND q.created_at >= sqlc.arg('since')::timestamptz;

-- name: CountAgentTasksSince :one
SELECT count(*) FILTER (WHERE status = 'failed')::bigint AS failed, count(*)::bigint AS total
FROM agent_task_queue WHERE agent_id = $1 AND created_at >= sqlc.arg('since')::timestamptz AND status IN ('completed', 'failed');

-- name: CountAgentOpenTasks :one
SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND status IN ('queued', 'dispatched', 'running');

-- name: CountOpenIssuesByProject :one
SELECT count(*) FROM issue WHERE workspace_id = $1 AND project_id = $2 AND NOT (status = ANY(sqlc.arg('terminal_status_keys')::text[]));

-- name: ListOpenIssuesRoutedToUnit :many
SELECT i.* FROM issue i
WHERE i.workspace_id = $1 AND NOT (i.status = ANY(sqlc.arg('terminal_status_keys')::text[]))
  AND i.id IN (SELECT f.issue_id FROM org_flow f WHERE f.structure_id = $2 AND f.unit_id = sqlc.arg('unit_id')::text AND f.kind = 'routing')
LIMIT 200;

-- name: PurgeWorkspaceOrg :exec
WITH f AS (DELETE FROM org_flow WHERE org_flow.workspace_id = $1),
     o AS (DELETE FROM org_offer WHERE org_offer.workspace_id = $1),
     r AS (DELETE FROM org_revision WHERE org_revision.workspace_id = $1)
DELETE FROM org_structure WHERE org_structure.workspace_id = $1;

-- name: ListRecentTasksForProject :many
SELECT q.* FROM agent_task_queue q JOIN issue i ON i.id = q.issue_id
WHERE i.workspace_id = $1 AND i.project_id = $2 AND q.status IN ('completed', 'failed')
ORDER BY q.created_at DESC LIMIT 1;

-- name: GetLatestOrgRoutingForIssue :one
SELECT * FROM org_flow WHERE issue_id = $1 AND kind IN ('routing', 'escalation') ORDER BY created_at DESC LIMIT 1;

-- name: SetOrgOfferStatus :exec
UPDATE org_offer SET status = $2 WHERE id = $1;
