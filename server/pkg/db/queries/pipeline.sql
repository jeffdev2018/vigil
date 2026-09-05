-- Pipelines (K37).

-- name: CreatePipeline :one
INSERT INTO pipeline (id, workspace_id, name, created_by) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdatePipelineName :one
UPDATE pipeline SET name = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ArchivePipeline :exec
UPDATE pipeline SET archived_at = now(), updated_at = now() WHERE id = $1;

-- name: GetPipeline :one
SELECT * FROM pipeline WHERE id = $1;

-- name: ListPipelines :many
SELECT * FROM pipeline WHERE workspace_id = $1 AND archived_at IS NULL ORDER BY created_at, id;

-- name: CreatePipelineStage :one
INSERT INTO pipeline_stage (id, pipeline_id, workspace_id, position, name, executor_type, executor_id, requires_human_gate)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: DeletePipelineStages :exec
DELETE FROM pipeline_stage WHERE pipeline_id = $1;

-- name: ListPipelineStages :many
SELECT * FROM pipeline_stage WHERE pipeline_id = $1 ORDER BY position;

-- name: GetPipelineStage :one
SELECT * FROM pipeline_stage WHERE id = $1;

-- name: CreatePipelineRun :one
INSERT INTO pipeline_run (id, workspace_id, issue_id, pipeline_id, current_stage_id, started_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetPipelineRun :one
SELECT * FROM pipeline_run WHERE id = $1;

-- name: GetOpenPipelineRunForIssue :one
SELECT * FROM pipeline_run WHERE issue_id = $1 AND status IN ('active', 'paused') ORDER BY started_at DESC LIMIT 1;

-- name: GetLatestPipelineRunForIssue :one
SELECT * FROM pipeline_run WHERE issue_id = $1 ORDER BY started_at DESC LIMIT 1;

-- name: GetPipelineRunByGateDecision :one
SELECT * FROM pipeline_run WHERE gate_decision_id = $1 AND status = 'paused';

-- name: CountOpenPipelineRunsForPipeline :one
SELECT count(*) FROM pipeline_run WHERE pipeline_id = $1 AND status IN ('active', 'paused');

-- name: SetPipelineRunStage :one
UPDATE pipeline_run SET current_stage_id = $2, status = 'active', gate_decision_id = NULL, last_error = NULL WHERE id = $1 RETURNING *;

-- name: SetPipelineRunGate :one
UPDATE pipeline_run SET status = 'paused', gate_decision_id = $2, current_stage_id = $3 WHERE id = $1 RETURNING *;

-- name: SetPipelineRunError :one
UPDATE pipeline_run SET status = 'paused', last_error = $2 WHERE id = $1 RETURNING *;

-- name: FinishPipelineRun :one
UPDATE pipeline_run SET status = $2, completed_at = now(), gate_decision_id = NULL WHERE id = $1 RETURNING *;

-- name: SetIssueAssigneeForPipeline :one
UPDATE issue SET assignee_type = $2, assignee_id = $3, updated_at = now() WHERE id = $1 RETURNING *;

-- name: PurgeWorkspacePipelineRuns :exec
DELETE FROM pipeline_run WHERE workspace_id = $1;

-- name: PurgeWorkspacePipelineStages :exec
DELETE FROM pipeline_stage WHERE workspace_id = $1;

-- name: PurgeWorkspacePipelines :exec
DELETE FROM pipeline WHERE workspace_id = $1;
