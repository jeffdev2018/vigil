-- Issue transition rules and approval requests (F28).

-- name: ListIssueTransitionRules :many
-- Every rule in the workspace, newest last so the editor renders a stable
-- order. The resolver filters by target category itself: a workspace has a
-- handful of rules, and one read that the caller can cache beats a query per
-- candidate category on the effective endpoint.
SELECT * FROM issue_transition_rule
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListEnabledIssueTransitionRulesForTarget :many
-- The gate's read. Scoped to the one target category the write is aiming at
-- and to rules that apply to this issue: workspace-wide (project_id IS NULL)
-- or pinned to the issue's project. Matches idx_issue_transition_rule_lookup.
SELECT * FROM issue_transition_rule
WHERE workspace_id = $1
  AND enabled
  AND to_category = $2
  AND (project_id IS NULL OR project_id = sqlc.narg('project_id')::uuid)
ORDER BY created_at ASC;

-- name: ListEnabledIssueTransitionRulesForIssue :many
-- The effective endpoint's read: every enabled rule that could apply to this
-- issue, across all target categories, in one round trip.
SELECT * FROM issue_transition_rule
WHERE workspace_id = $1
  AND enabled
  AND (project_id IS NULL OR project_id = sqlc.narg('project_id')::uuid)
ORDER BY created_at ASC;

-- name: GetIssueTransitionRule :one
SELECT * FROM issue_transition_rule WHERE id = $1 AND workspace_id = $2;

-- name: CreateIssueTransitionRule :one
INSERT INTO issue_transition_rule (
    id, workspace_id, project_id, from_category, to_category,
    allowed_roles, allow_actor_types, requires_approval, approver_roles,
    reject_status_key, enabled, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: UpdateIssueTransitionRule :one
UPDATE issue_transition_rule
SET from_category     = sqlc.narg('from_category'),
    to_category       = COALESCE(sqlc.narg('to_category'), to_category),
    allowed_roles     = COALESCE(sqlc.narg('allowed_roles')::text[], allowed_roles),
    allow_actor_types = COALESCE(sqlc.narg('allow_actor_types')::text[], allow_actor_types),
    requires_approval = COALESCE(sqlc.narg('requires_approval'), requires_approval),
    approver_roles    = COALESCE(sqlc.narg('approver_roles')::text[], approver_roles),
    reject_status_key = sqlc.narg('reject_status_key'),
    enabled           = COALESCE(sqlc.narg('enabled'), enabled),
    project_id        = sqlc.narg('project_id'),
    updated_at        = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteIssueTransitionRule :execrows
DELETE FROM issue_transition_rule WHERE id = $1 AND workspace_id = $2;

-- name: ListIssueTransitionRuleActors :many
-- Nominative grants for a set of rules, read in one query so evaluating a
-- transition never becomes one round trip per candidate rule.
SELECT * FROM issue_transition_rule_actor
WHERE rule_id = ANY(sqlc.arg('rule_ids')::uuid[])
ORDER BY created_at ASC;

-- name: AddIssueTransitionRuleActor :exec
INSERT INTO issue_transition_rule_actor (id, rule_id, actor_type, actor_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (rule_id, actor_type, actor_id) DO NOTHING;

-- name: DeleteIssueTransitionRuleActors :exec
DELETE FROM issue_transition_rule_actor WHERE rule_id = $1;

-- name: ListSquadIDsForActor :many
-- Squads the acting member or agent belongs to, for the squad grants. Reads
-- squad_member only: a leader that is missing from the roster (the roster
-- insert is best-effort, see squad.go) is not granted, which fails closed.
SELECT s.id FROM squad s
JOIN squad_member sm ON sm.squad_id = s.id
WHERE s.workspace_id = $1 AND sm.member_type = $2 AND sm.member_id = $3;

-- name: CreateIssueTransitionRequest :one
INSERT INTO issue_transition_request (
    id, workspace_id, issue_id, from_status, to_status, rule_id,
    requested_by_type, requested_by_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetIssueTransitionRequest :one
SELECT * FROM issue_transition_request WHERE id = $1 AND workspace_id = $2;

-- name: GetPendingIssueTransitionRequestForIssue :one
SELECT * FROM issue_transition_request
WHERE workspace_id = $1 AND issue_id = $2 AND state = 'pending';

-- name: ListIssueTransitionRequestsForIssue :many
SELECT * FROM issue_transition_request
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY created_at DESC
LIMIT 50;

-- name: DecideIssueTransitionRequest :one
-- The concurrency fence. Two approvers racing both run this; the second one
-- matches no row and the handler turns that into 409 already_decided.
UPDATE issue_transition_request
SET state           = $3,
    decided_by_type = $4,
    decided_by_id   = $5,
    decided_at      = now(),
    note            = sqlc.narg('note')
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: PurgeWorkspaceIssueTransitionRules :exec
DELETE FROM issue_transition_rule WHERE workspace_id = $1;

-- name: PurgeWorkspaceIssueTransitionRuleActors :exec
DELETE FROM issue_transition_rule_actor
WHERE rule_id IN (SELECT id FROM issue_transition_rule WHERE workspace_id = $1);

-- name: PurgeWorkspaceIssueTransitionRequests :exec
DELETE FROM issue_transition_request WHERE workspace_id = $1;
