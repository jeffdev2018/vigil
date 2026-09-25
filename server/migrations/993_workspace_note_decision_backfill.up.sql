-- Backfill (JEF-415 / B04): every existing decision_record becomes a Brain
-- note, so the decision history a run already made is searchable and
-- injectable like any other note. Idempotent via the unique partial index on
-- decision_record_id (992): safe to run this file twice, or alongside the
-- runtime mirror in decision_record.go, without duplicating a note.
INSERT INTO workspace_note (
    id, workspace_id, title, content, source, kind,
    created_by_type, decision_record_id, created_at, updated_at
)
SELECT
    gen_random_uuid(),
    dr.workspace_id,
    LEFT(COALESCE(NULLIF(btrim(dr.title), ''), 'Décision'), 200),
    LEFT(
        '## Contexte' || E'\n\n' || dr.context ||
        E'\n\n## Décision' || E'\n\n' || dr.decision ||
        E'\n\n## Conséquences' || E'\n\n' || COALESCE(dr.consequences, ''),
        20000
    ),
    'decision',
    'decision',
    'system',
    dr.id,
    dr.created_at,
    dr.created_at
FROM decision_record dr
ON CONFLICT (decision_record_id) WHERE decision_record_id IS NOT NULL DO NOTHING;
