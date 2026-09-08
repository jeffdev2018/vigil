-- name: CreateAgentMemoryEvaluation :one
INSERT INTO agent_memory_evaluation (workspace_id, agent_id, memory_id, revision, memory_ids, report, report_hash, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetAgentMemoryEvaluation :one
SELECT * FROM agent_memory_evaluation WHERE id = $1 AND workspace_id = $2 AND memory_id = $3;

-- name: GetAgentMemoryEvaluationByHash :one
SELECT * FROM agent_memory_evaluation WHERE workspace_id = $1 AND memory_id = $2 AND report_hash = $3;

-- name: ListAgentMemoryEvaluations :many
SELECT * FROM agent_memory_evaluation WHERE workspace_id = $1 AND memory_id = $2
ORDER BY created_at DESC, id DESC LIMIT 10;

-- name: MarkAgentMemoryEvaluationAdopted :exec
UPDATE agent_memory_evaluation SET adopted_revision = $4
WHERE id = $1 AND workspace_id = $2 AND memory_id = $3;

-- name: DeleteAgentMemoryEvaluation :execrows
DELETE FROM agent_memory_evaluation WHERE id = $1 AND workspace_id = $2 AND memory_id = $3;

-- name: EnqueueAgentMemoryEvaluation :one
UPDATE agent_memory_evaluation SET execution_status='queued', execution_runtime_id=$2, execution_request_id=$3, execution_deadline=clock_timestamp()+interval '20 minutes'
WHERE id=$1 RETURNING *;

-- name: GetAgentMemoryEvaluationRequest :one
SELECT * FROM agent_memory_evaluation WHERE workspace_id=$1 AND memory_id=$2 AND execution_request_id=$3;

-- name: PeekAgentMemoryEvaluation :one
SELECT id FROM agent_memory_evaluation
WHERE execution_runtime_id=$1 AND execution_status='queued' AND execution_deadline>clock_timestamp()
ORDER BY created_at,id LIMIT 1;

-- name: ClaimAgentMemoryEvaluation :one
UPDATE agent_memory_evaluation SET execution_status='running'
WHERE id=$1 AND execution_runtime_id=$2 AND execution_status='queued' AND execution_deadline>clock_timestamp()
RETURNING *;

-- name: GetRuntimeMemoryEvaluation :one
SELECT * FROM agent_memory_evaluation WHERE id=$1 AND execution_runtime_id=$2 FOR UPDATE;

-- name: SaveRuntimeMemoryEvaluation :one
UPDATE agent_memory_evaluation SET report=$3, report_hash=$4, execution_status=$5
WHERE id=$1 AND execution_runtime_id=$2 AND execution_status='running' AND execution_deadline>clock_timestamp()
RETURNING *;

-- name: CancelAgentMemoryEvaluation :execrows
UPDATE agent_memory_evaluation SET execution_status='cancelled'
WHERE id=$1 AND workspace_id=$2 AND memory_id=$3 AND execution_status IN ('queued','running');
