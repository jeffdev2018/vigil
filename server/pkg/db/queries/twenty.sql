-- Twenty CRM integration (OS plan, chantier 2).

-- name: GetWorkspaceTwentyConnection :one
SELECT * FROM workspace_twenty_connection WHERE workspace_id = $1;

-- name: UpsertWorkspaceTwentyConnection :one
INSERT INTO workspace_twenty_connection (id, workspace_id, base_url, api_key_sealed, webhook_secret_sealed, inbound_token_sealed, twenty_webhook_id, events, expose_to_agents, status, last_error, twenty_workspace_name, created_by_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'connected', '', $10, $11)
ON CONFLICT (workspace_id) DO UPDATE SET
    base_url = EXCLUDED.base_url,
    api_key_sealed = EXCLUDED.api_key_sealed,
    webhook_secret_sealed = EXCLUDED.webhook_secret_sealed,
    inbound_token_sealed = EXCLUDED.inbound_token_sealed,
    twenty_webhook_id = EXCLUDED.twenty_webhook_id,
    events = EXCLUDED.events,
    expose_to_agents = EXCLUDED.expose_to_agents,
    status = 'connected',
    last_error = '',
    twenty_workspace_name = EXCLUDED.twenty_workspace_name,
    updated_at = now()
RETURNING *;

-- name: UpdateWorkspaceTwentyConnectionSettings :one
UPDATE workspace_twenty_connection
SET events = $2, expose_to_agents = $3, updated_at = now()
WHERE workspace_id = $1
RETURNING *;

-- name: UpdateWorkspaceTwentyConnectionWebhook :one
UPDATE workspace_twenty_connection
SET twenty_webhook_id = $2, webhook_secret_sealed = $3, updated_at = now()
WHERE workspace_id = $1
RETURNING *;

-- name: SetWorkspaceTwentyConnectionStatus :one
UPDATE workspace_twenty_connection
SET status = $2, last_error = $3, updated_at = now()
WHERE workspace_id = $1
RETURNING *;

-- name: DeleteWorkspaceTwentyConnection :exec
DELETE FROM workspace_twenty_connection WHERE workspace_id = $1;

-- name: PurgeWorkspaceTwentyConnections :exec
DELETE FROM workspace_twenty_connection WHERE workspace_id = $1;

-- name: GetTriageItemWithSourceKind :one
SELECT i.id, i.workspace_id, i.title, i.payload, i.issue_id, s.kind AS source_kind
FROM triage_item i JOIN triage_source s ON s.id = i.source_id
WHERE i.id = $1;
