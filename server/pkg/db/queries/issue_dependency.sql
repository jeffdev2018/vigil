-- issue_dependency links two issues of the same workspace.
--
-- Only one direction is stored. A row (issue_id = A, depends_on_issue_id = B,
-- type = 'blocks') means "A blocks B"; "B is blocked by A" is the same row read
-- from B's side, never a second row with type 'blocked_by'. `related` rows are
-- symmetric and stored once, in whichever orientation they were created. The
-- column name depends_on_issue_id predates this convention; `type` names the
-- relation from issue_id to depends_on_issue_id.

-- name: CreateIssueDependency :one
INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetIssueDependency :one
SELECT * FROM issue_dependency
WHERE issue_id = $1 AND depends_on_issue_id = $2 AND type = $3;

-- name: DeleteIssueDependency :one
DELETE FROM issue_dependency
WHERE id = $1
  AND (issue_id = $2 OR depends_on_issue_id = $2)
RETURNING *;

-- name: ListIssueDependenciesForIssue :many
-- Every row touching $1, with the other issue embedded. `direction` tells the
-- caller which side $1 sits on: 'blocks' when $1 is issue_id, 'blocked_by'
-- when $1 is depends_on_issue_id ('related' stays 'related' either way).
SELECT d.id, d.type,
       CASE
         WHEN d.type <> 'blocks' THEN d.type
         WHEN d.issue_id = $1 THEN 'blocks'
         ELSE 'blocked_by'
       END::text AS direction,
       sqlc.embed(i)
FROM issue_dependency d
JOIN issue i ON i.id = CASE WHEN d.issue_id = $1 THEN d.depends_on_issue_id ELSE d.issue_id END
WHERE d.issue_id = $1 OR d.depends_on_issue_id = $1
ORDER BY i.number ASC;

-- name: ListIssueDependencyStack :many
-- Issues transitively blocked by $1 (following 'blocks' edges downward),
-- bounded to $2 levels. Shared by the anti-cycle check and the PR stack (F10).
-- ponytail: a cycle deeper than the bound is invisible; raise the bound before
-- adding a visited set.
WITH RECURSIVE stack AS (
    SELECT d.depends_on_issue_id AS issue_id, 1 AS depth
    FROM issue_dependency d
    WHERE d.issue_id = sqlc.arg(issue_id) AND d.type = 'blocks'
  UNION
    SELECT d.depends_on_issue_id, s.depth + 1
    FROM issue_dependency d
    JOIN stack s ON d.issue_id = s.issue_id
    WHERE d.type = 'blocks' AND s.depth < sqlc.arg(max_depth)::int
)
SELECT issue_id, MIN(depth)::int AS depth
FROM stack
GROUP BY issue_id
ORDER BY depth ASC;

-- name: BumpIssueRevisions :exec
UPDATE issue SET revision = revision + 1, updated_at = now()
WHERE id = ANY($1::uuid[]);
