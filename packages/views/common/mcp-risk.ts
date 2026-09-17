import type { McpToolRisk } from "@multica/core/types";

/**
 * The MCP risk vocabulary as the UI presents it (K77). Shared because three
 * surfaces read the same classification — the settings catalogue editor, an
 * agent's tool policy, and the workspace tools page — and a risk that looks
 * destructive on one screen and neutral on another is worse than no badge.
 *
 * Order matches the server's `mcpgov.Risks`: least to most consequential,
 * `unknown` last.
 */
export const MCP_TOOL_RISKS: McpToolRisk[] = [
  "read",
  "internal_write",
  "external_effect",
  "sensitive_data",
  "unknown",
];

/** Badge variant per risk. `unknown` is not reassuring: it is unclassified. */
export const MCP_RISK_BADGE: Record<
  McpToolRisk,
  "outline" | "secondary" | "destructive"
> = {
  read: "outline",
  internal_write: "secondary",
  external_effect: "destructive",
  sensitive_data: "destructive",
  unknown: "secondary",
};
