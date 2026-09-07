ALTER TABLE issue_view DROP CONSTRAINT IF EXISTS issue_view_scope_type_check;
ALTER TABLE issue_view ADD CONSTRAINT issue_view_scope_type_check
    CHECK (scope_type IN ('workspace', 'my', 'project'));

ALTER TABLE issue_view DROP CONSTRAINT IF EXISTS issue_view_check;
ALTER TABLE issue_view ADD CONSTRAINT issue_view_check
    CHECK (
        (scope_type = 'project' AND scope_id IS NOT NULL)
        OR (scope_type IN ('workspace', 'my') AND scope_id IS NULL)
    );

ALTER TABLE issue_view DROP CONSTRAINT IF EXISTS issue_view_scope_variant_pairing;
ALTER TABLE issue_view ADD CONSTRAINT issue_view_scope_variant_pairing
    CHECK (
        (scope_type = 'my' AND scope_variant IN ('assigned', 'created', 'involved', 'any'))
        OR (scope_type = 'workspace' AND (scope_variant IS NULL OR scope_variant IN ('members', 'agents')))
        OR (scope_type = 'project' AND (scope_variant IS NULL OR scope_variant IN ('members', 'agents')))
    );
