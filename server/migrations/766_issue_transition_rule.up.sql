-- F28 (JEF-28): who may move an issue from one status CATEGORY to another, and
-- whether that move waits for an approver.
--
-- The rule is written against CATEGORIES, never status keys. A workspace that
-- renames "In progress" or adds a custom status in the in_progress category
-- keeps its rules working, and a rule cannot be silently voided by archiving
-- the one status it named. reject_status_key is the single exception: it is a
-- concrete destination a rejected request lands on, so it has to name a key.
--
-- project_id NULL is the workspace-wide rule; a row with a project_id
-- overrides it for that project only. from_category NULL means "from any
-- origin"; an explicit from_category wins over it for the same (project,
-- to_category). The resolver in internal/issuestatus/transition.go owns that
-- precedence and states it in one place.
--
-- No FOREIGN KEY, per the repository rule. workspace_id, project_id and
-- created_by are validated in application code, and the rows are removed with
-- the workspace in the delete graph.
--
-- allowed_roles / allow_actor_types are the broad grants; the nominative
-- grants live in issue_transition_rule_actor (768). A rule with all three
-- empty grants nobody, which is how a category is locked down entirely.
CREATE TABLE IF NOT EXISTS issue_transition_rule (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    project_id        UUID,
    from_category     TEXT,
    to_category       TEXT NOT NULL,
    allowed_roles     TEXT[] NOT NULL DEFAULT '{}',
    allow_actor_types TEXT[] NOT NULL DEFAULT '{}',
    requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    approver_roles    TEXT[] NOT NULL DEFAULT '{}',
    reject_status_key TEXT,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    created_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_transition_rule_actor_types_check
        CHECK (allow_actor_types <@ ARRAY['member', 'agent', 'squad']::TEXT[])
);

COMMENT ON TABLE issue_transition_rule IS
    'F28: one workspace (project_id NULL) or per-project rule saying who may move an issue into to_category, and whether the move needs approval. No FK by house rule.';
