-- Per-message human attribution for multiplayer chat sessions (K31 / JEF-181).
-- Nullable on purpose: assistant rows and every message written before this
-- migration stay NULL, and the client falls back to the session creator only
-- when the session is solo. Nothing is backfilled.
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS author_user_id UUID;
