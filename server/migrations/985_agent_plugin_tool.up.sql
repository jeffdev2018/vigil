-- Deny by default: an installed, enabled plugin's agent-tool hooks and mcp
-- tools used to be offered to every agent in the workspace just because the
-- workspace had the plugin installed. This table is the opt-in — an agent
-- sees a plugin's hook as a callable tool only once someone binds it to that
-- agent, exactly as an agent's MCP settings already require a connection to
-- be added rather than assuming every workspace connection is available to
-- every agent.
--
-- No foreign keys per repository policy: relationships are resolved and
-- cleaned up in application code. hook_key names a hook contributed by
-- installation_id's manifest at bind time; a manifest upgrade that drops the
-- hook simply makes this binding unreachable (AgentHookTools / the mcp
-- connection builder both re-check the current manifest), not dangling in a
-- way that needs its own cleanup.
CREATE TABLE agent_plugin_tool (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    installation_id UUID NOT NULL,
    hook_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
