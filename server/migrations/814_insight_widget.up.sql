-- F27 (JEF-25): a pinned insight. The user asks a question in plain language,
-- the assist layer turns it into a document conforming to the closed insight
-- DSL, and pinning stores that document. The widget then refreshes through
-- POST /api/insights/run, which compiles and executes the stored document —
-- the model is never called again, so a pinned figure cannot silently change
-- meaning because a translation drifted.
--
-- `query` is the DSL document, NOT SQL. Nothing in this column is ever
-- executed as text: the server compiles it, validating every enum against an
-- allowlist and binding every value as a placeholder.
--
-- `question` is stored beside it because every card must show the question that
-- produced it. A figure whose question is invisible is a figure nobody can
-- check, and that is the only real defence against a plausible wrong number.
--
-- `revision` is the optimistic-concurrency token PATCH matches with
-- expected_revision, so two people editing the same widget cannot silently
-- clobber each other.
--
-- No FOREIGN KEY, per the repository rule; workspace teardown purges by
-- workspace_id and the handler resolves ownership in application code.
CREATE TABLE IF NOT EXISTS insight_widget (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    question TEXT NOT NULL DEFAULT '',
    definition_version INTEGER NOT NULL DEFAULT 1,
    query JSONB NOT NULL,
    display JSONB NOT NULL DEFAULT '{}'::jsonb,
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'workspace')),
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(query) = 'object'),
    CHECK (jsonb_typeof(display) = 'object')
);

COMMENT ON TABLE insight_widget IS
    'F27: one pinned insight. query is a closed-DSL document the server compiles, never SQL. No FK by house rule.';
COMMENT ON COLUMN insight_widget.question IS
    'The plain-language question that produced the document. Always rendered on the card.';
COMMENT ON COLUMN insight_widget.definition_version IS
    'DSL version the document was written against, so a future widening can migrate stored documents instead of guessing.';
