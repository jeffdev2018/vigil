-- Cross-repo mirror issues (K54). A mirror link is configured per
-- (source project -> target project) with a trigger label: when an issue of
-- the source project carries that label, one mirror issue is created in each
-- configured target project and linked so the mirror BLOCKS the source.
--
-- No foreign keys by repository convention. Workspace teardown deletes both
-- tables explicitly (purge steps in handler/workspace.go); deleting a link
-- deliberately keeps the mirrors it already produced, because those are real
-- issues someone may already be working on.
CREATE TABLE IF NOT EXISTS project_mirror_link (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    source_project_id UUID NOT NULL,
    target_project_id UUID NOT NULL,
    -- Label NAME, not id: the trigger is the human-facing label text, so a
    -- label deleted and recreated with the same name keeps working.
    trigger_label     TEXT NOT NULL CHECK (trigger_label <> ''),
    created_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_mirror_link_distinct_projects CHECK (source_project_id <> target_project_id)
);

-- One row per mirror issue produced from a source issue through a link.
-- type_synced is an independent marker per mirror: the two issues live in
-- different projects with different type catalogs, so "the types now match"
-- is a human decision, tracked here rather than derived.
CREATE TABLE IF NOT EXISTS issue_mirror (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    source_issue_id UUID NOT NULL,
    mirror_issue_id UUID NOT NULL,
    link_id         UUID NOT NULL,
    type_synced     BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
