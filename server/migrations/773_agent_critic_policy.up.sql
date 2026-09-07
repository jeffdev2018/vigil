-- F25 (JEF-18): the adversarial critic policy of ONE agent or ONE squad.
--
-- K15's cross-review is a workspace/project policy: every code run of a
-- covered project gets a second reader. This is the other axis — a team
-- decides that THIS agent (or THIS squad) never delivers unreviewed, whatever
-- project the issue belongs to. The two coexist: a run can be both
-- cross-reviewed and criticised, and neither knows about the other.
--
-- critic_agent_id is deliberately NOT NULL-with-a-default-picker: an enabled
-- policy names its critic, because "whichever agent the platform picks" is
-- exactly K15's job and F25 exists to be the explicit choice. The handler
-- refuses enabling without one (422 critic_required).
--
-- blocking = FALSE is the default on purpose: the first thing a workspace
-- wants is the second opinion, not a new way for an issue to get stuck.
--
-- phases is an array so a later phase (plan, test) can be added without a
-- migration; only 'change' is acted on today.
--
-- No FOREIGN KEY, per the repository rule. workspace_id, subject_id and
-- critic_agent_id are validated in application code and removed with the
-- workspace in the delete graph.
CREATE TABLE IF NOT EXISTS agent_critic_policy (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL,
    subject_type             TEXT NOT NULL CHECK (subject_type IN ('agent', 'squad')),
    subject_id               UUID NOT NULL,
    enabled                  BOOLEAN NOT NULL DEFAULT FALSE,
    critic_agent_id          UUID,
    require_distinct_provider BOOLEAN NOT NULL DEFAULT TRUE,
    blocking                 BOOLEAN NOT NULL DEFAULT FALSE,
    max_rounds               SMALLINT NOT NULL DEFAULT 1,
    max_cost_usd_ticks       BIGINT,
    phases                   TEXT[] NOT NULL DEFAULT '{change}',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE agent_critic_policy IS
    'F25: per-agent / per-squad adversarial critic policy. One row per subject; the unique index of 774 enforces it. No FK by house rule.';
