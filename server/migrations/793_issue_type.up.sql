-- Work item types (F30 / JEF-34): a per-workspace catalogue of what an issue
-- IS — bug, story, epic, task, plus whatever else the workspace defines.
--
-- MODEL: `issue.issue_type` stays a plain nullable TEXT column holding a key
-- from this catalogue, exactly as `issue.status` holds a status key. NULL means
-- UNTYPED and is the state every pre-existing issue is in: there is no backfill
-- and no default type, because guessing a type for an issue nobody classified
-- would be indistinguishable from one somebody did.
--
-- Unlike a status, a type carries NO platform behavior. It groups, it filters,
-- and it scopes which custom properties apply. That is deliberate: the moment a
-- type decides what an agent does, changing one silently rewrites the machine
-- semantics of every issue on it, which is the exact hazard the status
-- catalogue had to design around.
--
-- No foreign keys (workspace_id is an application-layer relation, per the
-- project's database rules); cleanup is handled by the application.
CREATE TABLE IF NOT EXISTS issue_type (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,

    -- Stable machine handle. Immutable after create: it is what
    -- `issue.issue_type` stores, what the API accepts, and what a property
    -- scope references.
    key TEXT NOT NULL CHECK (key ~ '^[a-z0-9][a-z0-9_]{0,31}$'),

    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 256),
    color TEXT NOT NULL CHECK (color ~ '^#[0-9a-f]{6}$'),

    -- Catalogue key from `validPropertyIcons`, so the four seeded types and any
    -- custom one render through the same glyph map the property picker uses.
    icon TEXT NOT NULL DEFAULT '',

    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Archiving a seeded type would retire a handle the product documents and
    -- agents reference. Enforced in storage so a handler bug cannot do it.
    CONSTRAINT issue_type_system_not_archivable
        CHECK (NOT is_system OR archived_at IS NULL)
);

COMMENT ON TABLE issue_type IS
    'F30: per-workspace work item type catalogue. issue.issue_type holds a key from here; NULL means untyped.';

-- Property scoping. A property with NO row here is GLOBAL: it applies to every
-- type and to untyped issues. One or more rows narrow it to those types only.
--
-- workspace_id is denormalized (derivable through property_id) so workspace
-- teardown and the "every query filters by workspace_id" rule hold here too.
CREATE TABLE IF NOT EXISTS issue_property_type (
    workspace_id UUID NOT NULL,
    property_id UUID NOT NULL,
    type_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (property_id, type_key)
);

COMMENT ON TABLE issue_property_type IS
    'F30: which work item types a custom property applies to. No row for a property = global.';

-- NULL = untyped. No backfill and no DEFAULT: an issue that was never
-- classified must stay distinguishable from one that was.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS issue_type TEXT;

-- issue_dependency predates created_at (migration 001). The Gantt orders its
-- arrows deterministically and the dependency list wants a creation order, so
-- add it now; existing rows take the migration timestamp, which is the closest
-- honest answer available.
ALTER TABLE issue_dependency ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
