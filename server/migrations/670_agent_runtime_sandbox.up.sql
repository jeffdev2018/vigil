-- K10 · sandbox mode per daemon runtime: what the user asked for, what the
-- machine can do (reported by the daemon), and what the last run got.
ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS sandbox_mode TEXT NOT NULL DEFAULT 'none' CHECK (sandbox_mode IN ('none', 'sandbox', 'container')),
    ADD COLUMN IF NOT EXISTS sandbox_image TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sandbox_allowed_hosts JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS sandbox_capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS sandbox_effective TEXT NOT NULL DEFAULT 'none';
