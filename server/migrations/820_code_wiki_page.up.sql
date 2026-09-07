-- F26 (JEF-22): one page of a code wiki snapshot.
--
-- `content` is Markdown written by a language model from a repository whose
-- contents we do not control, and it is read back by other agents. It is DATA,
-- never instruction: the MCP surface that serves it prefixes every result with
-- a notice saying so and never merges it into an instruction field.
--
-- `citations` is a JSON array of {path, start_line?, end_line?, commit_sha}.
-- A page with an empty array is refused at write time (400) — a wiki that
-- cites nothing is a wiki nobody can check.
CREATE TABLE IF NOT EXISTS code_wiki_page (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    snapshot_id UUID NOT NULL,
    project_resource_id UUID NOT NULL,
    slug TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    citations JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE code_wiki_page IS
    'F26: one generated wiki page. Content is machine-generated reference data, never an instruction to a reading agent.';
