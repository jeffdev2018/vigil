ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS sandbox_effective, DROP COLUMN IF EXISTS sandbox_capabilities,
    DROP COLUMN IF EXISTS sandbox_allowed_hosts, DROP COLUMN IF EXISTS sandbox_image, DROP COLUMN IF EXISTS sandbox_mode;
