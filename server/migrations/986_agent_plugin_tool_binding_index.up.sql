-- One binding per (agent, installation, hook): BindAgentPluginTool relies on
-- this for its ON CONFLICT DO NOTHING to be idempotent rather than creating a
-- duplicate row every time an admin re-binds the same tool.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_plugin_tool_binding
    ON agent_plugin_tool (agent_id, installation_id, hook_key);
