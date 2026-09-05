-- Vigil learns you (K71).

-- name: UpsertWorkProfileObservation :one
INSERT INTO work_profile_observation (id, workspace_id, user_id, key, value, source, count, corrections, auto, state)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (workspace_id, user_id, key) DO UPDATE SET
    value = EXCLUDED.value, count = EXCLUDED.count, corrections = EXCLUDED.corrections, auto = EXCLUDED.auto, state = EXCLUDED.state,
    last_observed_at = now()
RETURNING *;

-- name: GetWorkProfileObservation :one
SELECT * FROM work_profile_observation WHERE workspace_id = $1 AND user_id = $2 AND key = $3;

-- name: GetWorkProfileObservationByID :one
SELECT * FROM work_profile_observation WHERE id = $1 AND workspace_id = $2;

-- name: ListWorkProfileObservations :many
SELECT * FROM work_profile_observation WHERE workspace_id = $1 AND user_id = $2 ORDER BY last_observed_at DESC LIMIT 200;

-- name: SetWorkProfileObservationAuto :one
UPDATE work_profile_observation SET auto = $3, state = $4 WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: DeleteWorkProfileObservation :execrows
DELETE FROM work_profile_observation WHERE id = $1 AND workspace_id = $2 AND user_id = $3;

-- name: CreateDecisionTrainingExample :one
INSERT INTO decision_training_example (id, workspace_id, user_id, decision_id, signature, question, options, option_id, modified_text, stake, auto)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (decision_id) DO NOTHING
RETURNING *;

-- name: GetDecisionTrainingExample :one
SELECT * FROM decision_training_example WHERE id = $1 AND workspace_id = $2;

-- name: ListDecisionTrainingExamples :many
-- The user's examples for one kind of decision, newest first, capped.
SELECT * FROM decision_training_example
WHERE workspace_id = $1 AND user_id = $2 AND signature = $3
ORDER BY answered_at DESC LIMIT 50;

-- name: CountDecisionTrainingExamples :one
SELECT COUNT(*)::bigint AS total,
       COUNT(*) FILTER (WHERE auto)::bigint AS auto_decided,
       COUNT(*) FILTER (WHERE overturned)::bigint AS overturned
FROM decision_training_example WHERE workspace_id = $1 AND user_id = $2;

-- name: OverturnDecisionTrainingExample :one
UPDATE decision_training_example SET overturned = true WHERE id = $1 AND workspace_id = $2 AND user_id = $3 RETURNING *;

-- name: CountRecentCorrections :one
-- Corrections over the last 30 days for one rule: auto-decided examples a
-- human overturned, over auto-decided examples.
SELECT COUNT(*) FILTER (WHERE auto)::bigint AS auto_decided,
       COUNT(*) FILTER (WHERE auto AND overturned)::bigint AS overturned
FROM decision_training_example
WHERE workspace_id = $1 AND user_id = $2 AND signature = $3 AND answered_at >= now() - interval '30 days';

-- name: DeleteWorkspaceWorkProfileObservations :exec
DELETE FROM work_profile_observation WHERE workspace_id = $1;

-- name: DeleteWorkspaceDecisionTrainingExamples :exec
DELETE FROM decision_training_example WHERE workspace_id = $1;

-- name: SetDecisionTrainingExampleAuto :exec
UPDATE decision_training_example SET auto = true WHERE decision_id = $1;
