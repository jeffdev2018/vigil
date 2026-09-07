-- Property → work item type scoping (F30).
--
-- A property with NO row here is GLOBAL: it applies to every type AND to
-- untyped issues. One or more rows narrow it to exactly those types — an
-- untyped issue then does not carry it, because "no type" cannot match a type
-- list.

-- name: ListIssuePropertyTypesForWorkspace :many
-- Every scope row in the workspace, so one round-trip answers "which types is
-- each property scoped to" for the whole settings page and the whole catalogue.
SELECT property_id, type_key FROM issue_property_type
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY property_id, type_key;

-- name: ListIssuePropertyTypeKeys :many
SELECT type_key FROM issue_property_type
WHERE property_id = sqlc.arg('property_id')::uuid
ORDER BY type_key;

-- name: DeleteIssuePropertyTypes :exec
DELETE FROM issue_property_type
WHERE property_id = sqlc.arg('property_id')::uuid;

-- name: InsertIssuePropertyTypes :exec
INSERT INTO issue_property_type (workspace_id, property_id, type_key)
SELECT sqlc.arg('workspace_id')::uuid, sqlc.arg('property_id')::uuid, k
FROM unnest(sqlc.arg('type_keys')::text[]) AS k
ON CONFLICT DO NOTHING;

-- name: DeleteIssuePropertyTypesForWorkspace :exec
DELETE FROM issue_property_type WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CountActivePropertiesApplicableToType :one
-- The per-type cap census: ACTIVE properties that are global (no scope row)
-- plus those scoped to this type. A property scoped elsewhere does not count
-- against this type's budget, which is the whole point of scoping.
SELECT COUNT(*)::bigint FROM issue_property p
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND p.archived_at IS NULL
  AND (
    NOT EXISTS (SELECT 1 FROM issue_property_type s WHERE s.property_id = p.id)
    OR EXISTS (
      SELECT 1 FROM issue_property_type s
      WHERE s.property_id = p.id AND s.type_key = sqlc.arg('type_key')::text
    )
  );

-- name: CountActiveGlobalProperties :one
-- ACTIVE properties with no scope row. They count against EVERY type's budget,
-- so this is the floor the cap check adds a type's own scoped count on top of.
SELECT COUNT(*)::bigint FROM issue_property p
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND p.archived_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM issue_property_type s WHERE s.property_id = p.id);
