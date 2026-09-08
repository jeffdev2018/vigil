-- One row per (session, user): adding an existing participant is idempotent,
-- and the access check reads at most one row. Also the index the per-session
-- participant list and the "am I a participant" EXISTS probe both scan.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_chat_session_participant
    ON chat_session_participant (chat_session_id, user_id);
