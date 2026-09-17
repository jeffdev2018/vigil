import type { McpCatalogTool, WorkspaceMcpServer } from "../types";

/**
 * Flattening and filtering for the workspace tools catalogue (JEF-426).
 *
 * The API answers per server — the library, then each server's tool
 * catalogue — while the page shows one row per TOOL with its server beside it.
 * That fold plus the search/filter matrix lives here so it can be tested
 * without a DOM, and so the page component stays wiring only.
 */

/** One catalogue row: a tool and the server that exposes it. */
export interface CatalogRow {
  server: WorkspaceMcpServer;
  tool: McpCatalogTool;
}

export interface CatalogFilters {
  /** Free text matched against the tool name and description. */
  query?: string;
  /** Server id, or "" for every server. */
  serverId?: string;
  /** Risk class, or "" for every risk. */
  risk?: string;
}

/**
 * Case- and accent-insensitive comparison key. NFD splits an accented letter
 * into its base letter plus a combining mark, and the range below drops the
 * marks — so "Résumé" and "resume" match, which is what a French-speaking
 * workspace expects from a search box.
 */
export function foldText(value: string): string {
  return value
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase();
}

/**
 * Every catalogued tool of the workspace, server by server, in the order the
 * servers were given. `toolsByServer` maps a server id to the tools fetched
 * for it; a server whose catalogue has not arrived yet contributes no row.
 */
export function buildCatalogRows(
  servers: WorkspaceMcpServer[],
  toolsByServer: Map<string, McpCatalogTool[]>,
): CatalogRow[] {
  const rows: CatalogRow[] = [];
  for (const server of servers) {
    for (const tool of toolsByServer.get(server.id) ?? []) {
      rows.push({ server, tool });
    }
  }
  return rows;
}

/** The rows matching every given filter. Filters are cumulative. */
export function filterCatalogRows(
  rows: CatalogRow[],
  { query = "", serverId = "", risk = "" }: CatalogFilters,
): CatalogRow[] {
  const needle = foldText(query.trim());
  return rows.filter(({ server, tool }) => {
    if (serverId !== "" && server.id !== serverId) return false;
    if (risk !== "" && tool.risk !== risk) return false;
    if (needle === "") return true;
    return (
      foldText(tool.name).includes(needle) ||
      foldText(tool.description ?? "").includes(needle)
    );
  });
}
