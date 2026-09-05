-- Skill Miner (K58).

-- name: CreateAgentCorrectionSignal :one
INSERT INTO agent_correction_signal (id, workspace_id, issue_id, agent_id, agent_comment_id, correction_comment_id, status_regressed)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (correction_comment_id) DO NOTHING
RETURNING *;

-- name: GetLatestAgentCommentBefore :one
-- The newest agent comment on the issue before @before, for the correction heuristic.
SELECT * FROM comment
WHERE issue_id = $1 AND author_type = 'agent' AND type = 'comment' AND created_at < @before::timestamptz
ORDER BY created_at DESC LIMIT 1;

-- name: ListUnminedCorrectionSignals :many
-- Signals not yet turned into a draft, with the correction text and the issue
-- title the miner clusters on. Older than @min_age so a thread settles first.
SELECT s.*, c.content AS correction_content, c.author_id AS corrector_id, i.title AS issue_title, i.number AS issue_number
FROM agent_correction_signal s
JOIN comment c ON c.id = s.correction_comment_id
JOIN issue i ON i.id = s.issue_id
WHERE s.mined_skill_id IS NULL AND s.detected_at < @min_age::timestamptz
ORDER BY s.workspace_id, s.agent_id, s.detected_at ASC
LIMIT 2000;

-- name: MarkCorrectionSignalsMined :exec
UPDATE agent_correction_signal SET mined_skill_id = $2 WHERE id = ANY($1::uuid[]);

-- name: ListCorrectionSignalsForSkill :many
SELECT s.*, i.number AS issue_number, i.title AS issue_title
FROM agent_correction_signal s JOIN issue i ON i.id = s.issue_id
WHERE s.mined_skill_id = $1 ORDER BY s.detected_at ASC;

-- name: ListDraftSkills :many
SELECT * FROM skill WHERE workspace_id = $1 AND status = 'draft' ORDER BY created_at DESC;

-- name: SetSkillStatus :one
UPDATE skill SET status = $3, updated_at = now() WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: CountDraftSkillsAmong :one
SELECT COUNT(*)::bigint FROM skill WHERE id = ANY($1::uuid[]) AND status = 'draft';

-- name: DeleteWorkspaceCorrectionSignals :exec
DELETE FROM agent_correction_signal WHERE workspace_id = $1;
