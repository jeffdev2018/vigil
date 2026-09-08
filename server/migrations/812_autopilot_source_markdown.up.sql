-- The DAEMON.md an autopilot was imported from (F24 / JEF-15), plus the digest
-- of that document.
--
-- source_markdown is kept verbatim so `GET /export` returns the declaration
-- the operator wrote rather than one reconstructed from columns — a
-- reconstruction would silently drop every field the schema has no home for
-- (notably `budget`, which the workspace-scoped run quota does not model
-- per-autopilot).
--
-- source_digest is the idempotence key: re-importing a byte-identical
-- document is a no-op that must not touch updated_at, so the digest is
-- compared before any write happens.
--
-- Both NULL on autopilots created through the API or the UI.
ALTER TABLE autopilot ADD COLUMN IF NOT EXISTS source_markdown TEXT;
ALTER TABLE autopilot ADD COLUMN IF NOT EXISTS source_digest TEXT;
