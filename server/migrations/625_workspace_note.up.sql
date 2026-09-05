-- Workspace Brain: a shared knowledge base of notes owned by the whole
-- workspace rather than one agent. Humans write them in the Brain page,
-- agents write them through `multica brain save`, and a daily curation pass
-- merges/retitles/tags/archives them. Every run gets the pinned + most
-- recent notes injected as files under .multica/knowledge/.
--
-- Ids are minted application-side as UUIDv7 (server/pkg/dbid) so inserts
-- cluster on the right edge of the primary-key B-tree.
CREATE TABLE workspace_note (
    id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '' CHECK (length(content) <= 20000),
    tags TEXT[] NOT NULL DEFAULT '{}',
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'agent', 'curation')),
    source_task_id UUID,
    source_agent_id UUID,
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    archived_at TIMESTAMPTZ,
    merged_into UUID,
    created_by_type TEXT NOT NULL DEFAULT 'member' CHECK (created_by_type IN ('member', 'agent', 'system')),
    created_by_id UUID,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(title) BETWEEN 1 AND 200)
);

COMMENT ON COLUMN workspace_note.source IS
    'Who produced this note: manual (a human), agent (a run via the CLI), curation (the daily pass).';
COMMENT ON COLUMN workspace_note.merged_into IS
    'Set on the archived source note when the curation pass folded it into another note; keeps the merge history without a FK.';
