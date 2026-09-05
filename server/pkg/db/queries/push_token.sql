-- Mobile push (K64).

-- name: UpsertMobilePushToken :one
INSERT INTO mobile_push_token (id, user_id, token, platform)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, token) DO UPDATE SET platform = EXCLUDED.platform, updated_at = now()
RETURNING *;

-- name: DeleteMobilePushToken :exec
DELETE FROM mobile_push_token WHERE user_id = $1 AND token = $2;

-- name: DeleteMobilePushTokenEverywhere :exec
-- A token Expo reports as unregistered is dead for every user.
DELETE FROM mobile_push_token WHERE token = $1;

-- name: ListMobilePushTokensForUsers :many
SELECT * FROM mobile_push_token WHERE user_id = ANY(sqlc.arg(user_ids)::uuid[]) ORDER BY created_at;

-- name: CountPendingDecisionInboxItems :one
-- Inbox zero (K63): the badge number — my unanswered Decision Cards.
SELECT count(*)::int FROM inbox_item i
JOIN issue_decision d ON (i.details->>'decision_id') ~ '^[0-9a-f-]{36}$' AND d.id = (i.details->>'decision_id')::uuid
WHERE i.workspace_id = $1 AND i.recipient_type = 'member' AND i.recipient_id = $2
  AND i.archived = false AND i.type IN ('decision_request', 'decision_escalated') AND d.response IS NULL;
