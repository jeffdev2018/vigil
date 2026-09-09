-- name: CreateChannelApprovalMessage :exec
-- Inline approvals in chat: remember where an ask was posted so the message can
-- be rewritten when the ask settles.
INSERT INTO channel_approval_message (
    id, workspace_id, installation_id, channel_type, chat_id, message_id, source, ask_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListPendingChannelApprovalMessages :many
SELECT * FROM channel_approval_message
WHERE workspace_id = $1 AND source = $2 AND ask_id = $3 AND settled = false
ORDER BY created_at;

-- name: MarkChannelApprovalMessageSettled :exec
UPDATE channel_approval_message SET settled = true WHERE id = $1;
