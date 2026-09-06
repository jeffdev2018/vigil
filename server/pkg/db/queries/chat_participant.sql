-- Multiplayer chat participants (K31 / JEF-181). The session creator is an
-- implicit owner with no row here, so every access check is
-- "creator_id = me OR a participant row exists".

-- name: AddChatSessionParticipant :one
-- Idempotent: re-adding an existing participant returns the existing row
-- rather than failing on uq_chat_session_participant.
INSERT INTO chat_session_participant (chat_session_id, user_id, role)
VALUES ($1, $2, COALESCE(sqlc.narg('role')::text, 'participant'))
ON CONFLICT (chat_session_id, user_id) DO UPDATE
    SET chat_session_id = EXCLUDED.chat_session_id
RETURNING *;

-- name: RemoveChatSessionParticipant :execrows
DELETE FROM chat_session_participant
WHERE chat_session_id = $1 AND user_id = $2;

-- name: DeleteChatSessionParticipantsBySession :exec
DELETE FROM chat_session_participant WHERE chat_session_id = $1;

-- name: IsChatSessionParticipant :one
SELECT EXISTS (
    SELECT 1 FROM chat_session_participant
    WHERE chat_session_id = $1 AND user_id = $2
);

-- name: ListChatSessionParticipants :many
-- Joined to "user" so the participant bar renders names and avatars without a
-- second round trip. A participant whose user row is gone is dropped by the
-- inner join; workspace teardown removes both together.
SELECT p.id,
       p.chat_session_id,
       p.user_id,
       p.role,
       p.joined_at,
       u.name,
       u.avatar_url
FROM chat_session_participant p
JOIN "user" u ON u.id = p.user_id
WHERE p.chat_session_id = $1
ORDER BY p.joined_at ASC, p.id ASC;
