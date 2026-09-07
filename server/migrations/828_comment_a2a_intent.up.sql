-- Agent-to-agent message intent (F19 / JEF-32).
--
-- An A2A message IS an ordinary comment: same author, same mentions, same
-- trigger path, same realtime and inbox fan-out. What it adds is a declared
-- intent — `question | review | handoff` — so a human reading the thread can
-- tell "@Bob please review" apart from discussion, and so the UI can chip it.
--
-- A marker column, not a new `comment_type_check` value, for the two reasons
-- migration 239 already settled for quick_action_id:
--
--   1. Re-adding the CHECK takes ACCESS EXCLUSIVE on `comment` while it scans
--      the whole table. `comment` is one of the hottest tables in the product.
--      A nullable column with no default is metadata-only and instant.
--
--   2. `type` is client-supplied on POST /comments, so any member could post
--      type='a2a_question' and have an ordinary comment render as a declared
--      agent request. There is NO request field for a2a_intent on that path —
--      only POST /issues/{id}/agent-messages sets it — so it cannot be forged.
--
-- Values are read as a free string with a `default` branch downstream: an
-- intent this build does not know renders as an ordinary comment rather than
-- an empty chip. No FK and no CHECK, per repo policy.
ALTER TABLE comment ADD COLUMN IF NOT EXISTS a2a_intent TEXT;

COMMENT ON COLUMN comment.a2a_intent IS
    'Agent-to-agent message intent: question | review | handoff. Written only by POST /api/issues/{id}/agent-messages; NULL on every other comment. An unknown value renders as an ordinary comment.';
