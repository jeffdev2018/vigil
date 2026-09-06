-- Multiplayer chat sessions (K31 / JEF-181). A chat_session used to be private
-- to its creator; this table lets other workspace members join the same
-- conversation with one agent.
--
-- The column is user_id, not member_id: chat_session.creator_id is a "user".id,
-- and access checks compare against the request's user id, so participants must
-- be keyed the same way. Workspace membership is validated in application code
-- when a participant is added.
--
-- The creator is an IMPLICIT owner and has no row here: every access check is
-- "creator OR participant row", so no backfill is needed for existing sessions.
-- The 'owner' role is reserved for a future ownership transfer; nothing writes
-- it today.
--
-- No foreign keys by repository convention: workspace teardown deletes these
-- rows explicitly (DeleteWorkspaceLeafData in queries/workspace_delete.sql).
CREATE TABLE IF NOT EXISTS chat_session_participant (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id UUID NOT NULL,
    user_id         UUID NOT NULL,
    role            TEXT NOT NULL DEFAULT 'participant'
                    CHECK (role IN ('owner', 'participant')),
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
