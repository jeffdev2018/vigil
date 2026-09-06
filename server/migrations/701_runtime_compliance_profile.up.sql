-- Data residency routing (K46). A workspace can declare where its work may
-- run — allowed regions, banned providers, on-prem only — and a run is then
-- dispatched only to a runtime that satisfies it. The policy itself lives
-- under workspace.settings.data_residency_policy; this table holds the other
-- half: what each runtime DECLARES about itself.
--
-- The declaration is operator-supplied, not verified: nothing here proves a
-- machine really sits in eu-west-1. It is a routing input, and the UI says so.
-- A runtime with no row is non-compliant as soon as the policy is restrictive
-- (fail closed), so an undeclared machine never receives regulated work by
-- default.
--
-- No foreign keys by repository convention: workspace teardown deletes these
-- rows explicitly (purge step in handler/workspace.go), and deleting one
-- runtime deletes its profile in the same transaction.
CREATE TABLE IF NOT EXISTS runtime_compliance_profile (
    -- One profile per runtime. The uniqueness is enforced by the concurrent
    -- unique index in migration 702, not by a PRIMARY KEY: a PK builds its
    -- index non-concurrently, which the repository migration rules forbid.
    runtime_id  UUID NOT NULL,
    region      TEXT NOT NULL CHECK (region <> ''),
    on_prem     BOOLEAN NOT NULL DEFAULT false,
    -- Who filed the declaration, for the audit trail. NULL when the row
    -- predates the actor or was written by the system.
    declared_by UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
