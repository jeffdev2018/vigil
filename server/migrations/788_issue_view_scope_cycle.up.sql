-- Saved views gain the cycle scope (F29): a cycle page carries the same
-- board / list / table surface as a project page, so it must be able to hold
-- saved views. Like a project view, a cycle view names its subject in
-- scope_id, and may narrow to the Members / Agents tab.
--
-- All three constraints are WIDENED only, so no existing row can fail the
-- scan; NOT VALID keeps this an instant catalog change and 789 validates.
ALTER TABLE issue_view DROP CONSTRAINT IF EXISTS issue_view_scope_type_check;
ALTER TABLE issue_view ADD CONSTRAINT issue_view_scope_type_check
    CHECK (scope_type IN ('workspace', 'my', 'project', 'cycle'))
    NOT VALID;

ALTER TABLE issue_view DROP CONSTRAINT IF EXISTS issue_view_check;
ALTER TABLE issue_view ADD CONSTRAINT issue_view_check
    CHECK (
        (scope_type IN ('project', 'cycle') AND scope_id IS NOT NULL)
        OR (scope_type IN ('workspace', 'my') AND scope_id IS NULL)
    )
    NOT VALID;

ALTER TABLE issue_view DROP CONSTRAINT IF EXISTS issue_view_scope_variant_pairing;
ALTER TABLE issue_view ADD CONSTRAINT issue_view_scope_variant_pairing
    CHECK (
        (scope_type = 'my' AND scope_variant IN ('assigned', 'created', 'involved', 'any'))
        OR (scope_type = 'workspace' AND (scope_variant IS NULL OR scope_variant IN ('members', 'agents')))
        OR (scope_type = 'project' AND (scope_variant IS NULL OR scope_variant IN ('members', 'agents')))
        OR (scope_type = 'cycle' AND (scope_variant IS NULL OR scope_variant IN ('members', 'agents')))
    )
    NOT VALID;
