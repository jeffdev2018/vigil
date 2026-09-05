-- Approval gates (K05).

-- name: CreateApprovalGateEvent :one
INSERT INTO approval_gate_event (id, workspace_id, task_id, issue_id, gate_type, decision_request_id, summary, details, resolved_action, expires_at, resolved_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetApprovalGateEvent :one
SELECT * FROM approval_gate_event WHERE id = $1 AND task_id = $2;

-- name: GetApprovalGateByDecision :one
-- A dual-approval gate (K07) files a second card, tracked in details.
SELECT * FROM approval_gate_event
WHERE decision_request_id = $1 OR details->>'pending_decision_id' = $1::text
LIMIT 1;

-- name: ResolveApprovalGateEvent :one
UPDATE approval_gate_event
SET resolved_action = $2, resolved_at = now(), details = details || sqlc.arg(extra)::jsonb
WHERE id = $1 AND resolved_action IS NULL
RETURNING *;

-- name: MarkSpendTokenUsed :one
UPDATE approval_gate_event
SET details = details || jsonb_build_object('used_at', now())
WHERE id = $1 AND gate_type = 'spend' AND resolved_action = 'approved' AND details->>'used_at' IS NULL
RETURNING *;

-- name: AttachSpendToken :exec
UPDATE approval_gate_event SET details = details || sqlc.arg(extra)::jsonb WHERE id = $1;

-- name: ListApprovalGateEvents :many
SELECT * FROM approval_gate_event WHERE task_id = $1 ORDER BY created_at DESC LIMIT 100;

-- name: PurgeWorkspaceApprovalGateEvents :exec
DELETE FROM approval_gate_event WHERE workspace_id = $1;
