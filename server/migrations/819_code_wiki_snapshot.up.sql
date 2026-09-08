-- F26 (JEF-22): one generated code wiki per project repo resource.
--
-- A snapshot is the unit of publication. A generation run creates it in
-- `building`, writes its pages, then flips it to `published` in one statement;
-- readers only ever select the newest `published` row for a resource, so a run
-- that dies half-way leaves the previous snapshot intact and visible.
--
-- `repo_paths` is the file inventory the generating run announced for
-- `commit_sha` (its `git ls-files`). It is the set a page citation is checked
-- against at write time: the server never opens the repository, so the only
-- honest way to refuse a citation to a file that does not exist is to compare
-- it with the inventory the run itself declared. Validating the announced path
-- rather than its content is deliberate — we verify the citation points
-- somewhere real, not that the prose is true.
--
-- No FOREIGN KEY, per the repository rule: the application purges these rows
-- when the project resource goes away, and `workspace_delete.sql` sweeps them
-- with the workspace.
CREATE TABLE IF NOT EXISTS code_wiki_snapshot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_resource_id UUID NOT NULL,
    commit_sha TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('building', 'published', 'failed')),
    generated_by_task_id UUID,
    page_count INT NOT NULL DEFAULT 0,
    repo_paths JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

COMMENT ON TABLE code_wiki_snapshot IS
    'F26: one atomically published generation of a repo wiki. repo_paths is the run-announced file inventory citations are validated against.';
