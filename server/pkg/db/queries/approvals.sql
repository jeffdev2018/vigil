-- Inline approvals (OS plan, chantier 3): everything a human is being asked
-- to decide, listed for one workspace or one issue.

-- name: ListPendingIssueDecisionsForWorkspace :many
SELECT * FROM issue_decision
WHERE workspace_id = $1 AND response IS NULL
ORDER BY created_at DESC
LIMIT $2;

-- name: ListPendingIssueTransitionRequestsForWorkspace :many
SELECT * FROM issue_transition_request
WHERE workspace_id = $1 AND state = 'pending'
ORDER BY created_at DESC
LIMIT 200;

-- name: ListWaitingIssueGoalsForWorkspace :many
SELECT * FROM issue_goal
WHERE workspace_id = $1 AND status = 'waiting_user' AND question IS NOT NULL
ORDER BY updated_at DESC
LIMIT 200;

-- name: ListExpiredPendingApprovalGates :many
-- The sweeper settles these; the lazy check on read stays for the run's own
-- long-poll.
SELECT * FROM approval_gate_event
WHERE resolved_action IS NULL AND expires_at IS NOT NULL AND expires_at <= $1
ORDER BY expires_at ASC
LIMIT $2;

-- name: RespondIssueDecisionAsSystem :one
-- The server answers a card on its own (a gate that timed out). Same
-- idempotent guard as RespondIssueDecision.
UPDATE issue_decision
SET response = $2, responded_by_type = 'system', responded_by_id = NULL, responded_at = now()
WHERE id = $1 AND response IS NULL
RETURNING *;
