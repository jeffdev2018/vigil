ALTER TABLE agent_mcp_server DROP COLUMN IF EXISTS tool_usage, DROP COLUMN IF EXISTS tool_policy;
ALTER TABLE workspace_mcp_server DROP COLUMN IF EXISTS tools_discovered_at, DROP COLUMN IF EXISTS tools;
