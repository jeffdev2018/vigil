-- Handoff packets (K17).

-- name: CreateHandoffPacket :one
INSERT INTO handoff_packet (id, run_id, workspace_id, issue_id, objective, decisions, evidence, failed_attempts, next_action, created_by_type, created_by_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListHandoffPackets :many
SELECT * FROM handoff_packet WHERE issue_id = $1 ORDER BY created_at ASC, id ASC LIMIT 100;

-- name: GetLatestHandoffPacket :one
SELECT * FROM handoff_packet WHERE issue_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: CountHandoffPacketsForRun :one
SELECT COUNT(*) FROM handoff_packet WHERE run_id = $1;

-- name: PurgeWorkspaceHandoffPackets :exec
DELETE FROM handoff_packet WHERE workspace_id = $1;
