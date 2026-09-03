-- Issue plans and their verification runs (F17). The active plan is the one
-- row per issue with superseded_at IS NULL; a verification is an ordinary
-- agent_task_queue run recognised by plan_verification.task_id.

-- name: GetActiveIssuePlan :one
SELECT * FROM issue_plan
WHERE issue_id = $1 AND workspace_id = $2 AND superseded_at IS NULL
ORDER BY version DESC
LIMIT 1;

-- name: GetIssuePlanVersion :one
SELECT * FROM issue_plan
WHERE issue_id = $1 AND workspace_id = $2 AND version = $3;

-- name: ListIssuePlanVersions :many
SELECT * FROM issue_plan
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY version DESC;

-- name: CreateIssuePlan :one
-- Next version is computed in the statement; the unique (issue_id, version)
-- index turns a concurrent publish into an error the handler retries once.
INSERT INTO issue_plan (workspace_id, issue_id, version, content, steps, author_type, author_id)
SELECT sqlc.arg(workspace_id), sqlc.arg(issue_id),
       COALESCE(MAX(version), 0) + 1,
       sqlc.arg(content), sqlc.arg(steps), sqlc.arg(author_type), sqlc.arg(author_id)
FROM issue_plan
WHERE issue_id = sqlc.arg(issue_id)
RETURNING *;

-- name: SupersedeOtherIssuePlans :exec
UPDATE issue_plan
SET superseded_at = now()
WHERE issue_id = $1 AND superseded_at IS NULL AND id <> $2;

-- name: CreatePlanVerification :one
INSERT INTO plan_verification (workspace_id, issue_id, plan_id, plan_version, task_id, source_task_id, state)
VALUES ($1, $2, $3, $4, $5, $6, 'queued')
RETURNING *;

-- name: GetPlanVerificationByTask :one
SELECT * FROM plan_verification WHERE task_id = $1;

-- name: PlanVerificationExistsForSource :one
-- True when the completed run already is a verification run, or already has one.
SELECT EXISTS (
    SELECT 1 FROM plan_verification WHERE task_id = $1 OR source_task_id = $1
);

-- name: ListPlanVerificationsByIssue :many
SELECT * FROM plan_verification
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at DESC
LIMIT 20;

-- name: GetLatestReportedPlanVerification :one
-- The gate reads the newest report for the ACTIVE plan; an older plan's
-- findings no longer block.
SELECT v.* FROM plan_verification v
JOIN issue_plan p ON p.id = v.plan_id
WHERE v.issue_id = $1 AND v.state = 'reported' AND p.superseded_at IS NULL
ORDER BY v.reported_at DESC
LIMIT 1;

-- name: SetPlanVerificationState :exec
UPDATE plan_verification SET state = $2
WHERE task_id = $1 AND state IN ('queued', 'running');

-- name: ReportPlanVerification :one
-- Idempotent: a second report for the same run matches no row.
UPDATE plan_verification
SET state = 'reported',
    findings = $2,
    critical_count = $3,
    major_count = $4,
    minor_count = $5,
    outdated_count = $6,
    summary = $7,
    reported_at = now()
WHERE task_id = $1 AND state IN ('queued', 'running')
RETURNING *;

-- name: TouchIssueRevision :one
-- Plan and verification changes live in side tables; bumping the issue
-- revision is what lets issue:updated carry them to clients.
UPDATE issue SET revision = revision + 1, updated_at = now()
WHERE id = $1
RETURNING *;
