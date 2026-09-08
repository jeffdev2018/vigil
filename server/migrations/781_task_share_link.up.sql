-- F12 (JEF-10), reused by F16: a scoped, expiring link to ONE run.
--
-- Distinct from workspace_share_link, which is an invite: that one hands the
-- visitor a membership, this one hands them exactly the capabilities listed
-- here and nothing else. Reusing the invite table would have made "can open the
-- preview" and "can join the workspace" the same grant.
--
-- `code` is the credential. It is looked up alone (782), so the resolver never
-- needs a workspace to find it, and an unknown / revoked / expired code is
-- answered with the same 404 as a code that never existed — a distinguishable
-- 403 would confirm which run ids are real.
--
-- capabilities is an array so F16's `view` / `steer` land without a migration;
-- only `preview` is honoured today.
--
-- No FOREIGN KEY, per the repository rule.
CREATE TABLE IF NOT EXISTS task_share_link (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    task_id       UUID NOT NULL,
    code          TEXT NOT NULL,
    capabilities  TEXT[] NOT NULL DEFAULT '{preview}',
    created_by    UUID,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ,
    use_count     BIGINT NOT NULL DEFAULT 0,
    last_used_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE task_share_link IS
    'F12: scoped, expiring share link to one run. The code is the credential; unique in 782. No FK by house rule.';
