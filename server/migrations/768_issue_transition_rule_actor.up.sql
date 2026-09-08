-- F28: the nominative half of a rule — this member, this agent, this squad may
-- make the transition, whatever the role and actor-type lists say.
--
-- A separate table rather than a third TEXT[] column because a grant is a pair
-- (type, id): storing it as an array would make "is this agent granted"
-- either a scan of two parallel arrays or a synthetic "agent:<uuid>" string
-- nobody can join on. One row per grant also lets the editor add and remove a
-- single actor without rewriting the rule.
--
-- No FOREIGN KEY, per the repository rule. rule_id is resolved in application
-- code; deleting a rule deletes its actor rows in the same transaction.
CREATE TABLE IF NOT EXISTS issue_transition_rule_actor (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id    UUID NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'squad')),
    actor_id   UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (rule_id, actor_type, actor_id)
);

COMMENT ON TABLE issue_transition_rule_actor IS
    'F28: one nominative grant on an issue_transition_rule. No FK by house rule.';
