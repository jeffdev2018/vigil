-- Contest (K72): a rival model challenges an agent output (run result, plan,
-- triage verdict, meeting summary) with numbered objections; the author
-- answers each; a human gives the verdict. Rounds are capped, the
-- challenger only reads. No foreign keys per the migration rules.
CREATE TABLE IF NOT EXISTS contest (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    project_id UUID,
    issue_id UUID,
    target_type TEXT NOT NULL CHECK (target_type IN ('task_result', 'plan', 'triage_verdict', 'meeting_summary')),
    target_id UUID NOT NULL,
    target_excerpt TEXT NOT NULL DEFAULT '',
    author_agent_id UUID,
    author_provider TEXT NOT NULL DEFAULT '',
    challenger_kind TEXT NOT NULL CHECK (challenger_kind IN ('agent', 'llm')),
    challenger_agent_id UUID,
    challenger_provider TEXT NOT NULL DEFAULT '',
    same_vendor BOOLEAN NOT NULL DEFAULT false,
    challenger_task_id UUID,
    answer_task_id UUID,
    round INTEGER NOT NULL DEFAULT 1,
    max_rounds INTEGER NOT NULL DEFAULT 1 CHECK (max_rounds BETWEEN 1 AND 2),
    objections JSONB NOT NULL DEFAULT '[]'::jsonb,
    answers JSONB NOT NULL DEFAULT '[]'::jsonb,
    nothing_to_contest TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'objections_ready', 'answering', 'answered', 'confirmed', 'failed')),
    human_verdict TEXT CHECK (human_verdict IN ('upheld', 'dismissed', 'mixed')),
    verdict_note TEXT NOT NULL DEFAULT '',
    confirmed_by UUID,
    confirmed_at TIMESTAMPTZ,
    auto BOOLEAN NOT NULL DEFAULT false,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
