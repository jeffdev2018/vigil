import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Vigil as an MCP server (OS plan, chantier 1): the workspace's settings for
// its own inbound MCP endpoint (POST /api/mcp/{slug}) — on/off, the default
// tool surface, and per-tool overrides — plus the catalogue and endpoint the
// settings page renders alongside them. See server/internal/handler/mcp_server.go
// and mcp_server_catalog.go for the source of truth.

/** The decision an override sets for one tool: allow, ask or deny. */
export type MCPToolDecision = "allow" | "ask" | "deny";

/** The tool surface a client gets when it asks for none. */
export type MCPServerSurface = "compound" | "granular";

export interface MCPServerSettings {
  /** Off refuses every call with a clear message; nothing else changes. */
  enabled: boolean;
  default_surface: MCPServerSurface;
  /** Per granular tool name. Can only tighten the caller's own ceiling. */
  tools: Record<string, MCPToolDecision>;
}

/** One row of the catalogue: a leaf tool, its risk class and grouping. */
export interface MCPServerCatalogTool {
  name: string;
  /** Compound tool name this leaf folds into (e.g. "vigil_issue"). */
  group: string;
  /** Action argument within the group on the compound surface. */
  action: string;
  risk: string;
  description: string;
  /** True for a tool only a run's own token may call (never a member's). */
  agent_only: boolean;
}

export interface MCPServerSettingsEnvelope {
  settings: MCPServerSettings;
  tools: MCPServerCatalogTool[];
  /** e.g. "/api/mcp/acme" — append to the API base URL for the full endpoint. */
  endpoint: string;
}

export const MCP_SERVER_SETTINGS_DEFAULT: MCPServerSettings = {
  enabled: true,
  default_surface: "compound",
  tools: {},
};

export const MCP_SERVER_SETTINGS_ENVELOPE_DEFAULT: MCPServerSettingsEnvelope = {
  settings: MCP_SERVER_SETTINGS_DEFAULT,
  tools: [],
  endpoint: "",
};

export const mcpServerKeys = {
  settings: (wsId: string) => ["mcp-server-settings", wsId] as const,
};

export function mcpServerSettingsOptions(wsId: string) {
  return queryOptions({ queryKey: mcpServerKeys.settings(wsId), queryFn: () => api.getMCPServerSettings() });
}

export function useUpdateMCPServerSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: MCPServerSettings) => api.putMCPServerSettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: mcpServerKeys.settings(wsId) }),
  });
}
