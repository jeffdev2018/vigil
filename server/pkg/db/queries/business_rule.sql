-- Business rules (K53).

-- name: CreateBusinessRule :one
INSERT INTO business_rule (id, workspace_id, title, natural_language, compiled_predicate, attach_point, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetBusinessRule :one
SELECT * FROM business_rule WHERE id = $1 AND workspace_id = $2;

-- name: ListBusinessRules :many
SELECT * FROM business_rule WHERE workspace_id = $1 ORDER BY created_at DESC, id DESC;

-- name: ListActiveBusinessRules :many
SELECT * FROM business_rule
WHERE workspace_id = $1 AND attach_point = $2 AND status = 'active'
ORDER BY created_at ASC, id ASC;

-- name: SetBusinessRuleStatus :one
UPDATE business_rule SET status = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteBusinessRule :exec
DELETE FROM business_rule WHERE id = $1 AND workspace_id = $2;

-- name: CreateBusinessRuleViolation :one
INSERT INTO business_rule_violation (id, rule_id, workspace_id, subject_type, subject_id, detail)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListBusinessRuleViolations :many
SELECT * FROM business_rule_violation WHERE rule_id = $1 AND workspace_id = $2
ORDER BY created_at DESC, id DESC LIMIT 50;

-- name: PurgeWorkspaceBusinessRules :exec
DELETE FROM business_rule WHERE workspace_id = $1;

-- name: PurgeWorkspaceBusinessRuleViolations :exec
DELETE FROM business_rule_violation WHERE workspace_id = $1;

-- Facts the predicates read.

-- name: CountWorkspaceProjects :one
SELECT COUNT(*) FROM project WHERE workspace_id = $1;

-- name: CountWorkspaceMembers :one
SELECT COUNT(*) FROM member WHERE workspace_id = $1;

-- name: CountWorkspaceAgents :one
SELECT COUNT(*) FROM agent WHERE workspace_id = $1;

-- name: CountIssueLabels :one
SELECT COUNT(*) FROM issue_to_label WHERE issue_id = $1;

-- name: CountIssuePullRequests :one
SELECT COUNT(*) FROM issue_vcs_pull_request WHERE issue_id = $1;

-- name: ListWorkspaceIssuesInReview :many
SELECT issue.* FROM issue
WHERE issue.workspace_id = $1
  AND (issue.status = 'in_review' OR issue.status IN (SELECT s.key FROM issue_status s WHERE s.workspace_id = $1 AND s.category = 'in_review'))
ORDER BY issue.updated_at DESC
LIMIT 100;
