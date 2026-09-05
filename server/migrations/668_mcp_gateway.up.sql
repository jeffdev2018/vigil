-- K77 · governed MCP gateway: a per-server tool catalogue classified by risk,
-- and a per-binding tool policy (approval class per tool) with usage tracking.
ALTER TABLE workspace_mcp_server
    ADD COLUMN IF NOT EXISTS tools JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS tools_discovered_at TIMESTAMPTZ;
ALTER TABLE agent_mcp_server
    ADD COLUMN IF NOT EXISTS tool_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS tool_usage JSONB NOT NULL DEFAULT '{}'::jsonb;
