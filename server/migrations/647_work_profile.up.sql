-- Vigil learns you (K71): per-person observed preferences and the decisions
-- that taught them. Personal data class: work preferences only. No FKs by
-- repo rule.
CREATE TABLE IF NOT EXISTS work_profile_observation (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    key TEXT NOT NULL,
    value JSONB NOT NULL DEFAULT '{}'::jsonb,
    source TEXT NOT NULL DEFAULT 'decisions',
    count INTEGER NOT NULL DEFAULT 0,
    corrections INTEGER NOT NULL DEFAULT 0,
    auto BOOLEAN NOT NULL DEFAULT false,
    state TEXT NOT NULL DEFAULT 'learned' CHECK (state IN ('learned', 'proposed')),
    first_observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id, key)
);

CREATE TABLE IF NOT EXISTS decision_training_example (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    decision_id UUID NOT NULL,
    signature TEXT NOT NULL,
    question TEXT NOT NULL DEFAULT '',
    options JSONB NOT NULL DEFAULT '[]'::jsonb,
    option_id TEXT NOT NULL DEFAULT '',
    modified_text TEXT NOT NULL DEFAULT '',
    stake TEXT NOT NULL DEFAULT 'normal' CHECK (stake IN ('normal', 'high')),
    auto BOOLEAN NOT NULL DEFAULT false,
    overturned BOOLEAN NOT NULL DEFAULT false,
    answered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (decision_id)
);
