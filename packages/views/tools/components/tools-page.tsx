"use client";

import { useState } from "react";
import { Plus, Search, Wrench } from "lucide-react";
import { useQueries, useQuery } from "@tanstack/react-query";
import type {
  McpCatalogTool,
  McpToolRisk,
  WorkspaceMcpServer,
} from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  workspaceMcpServerToolsOptions,
  workspaceMcpServersOptions,
} from "@multica/core/workspace/queries";
import {
  buildCatalogRows,
  filterCatalogRows,
} from "@multica/core/workspace/tool-catalog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { AppLink } from "../../navigation";
import { MCP_RISK_BADGE, MCP_TOOL_RISKS } from "../../common/mcp-risk";
import { useDebouncedValue } from "../../common/use-debounced-value";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../../layout/collection-page";
import { useT } from "../../i18n";
import { AddToAgentDialog } from "./add-to-agent-dialog";

/** Sentinel for "no filter" — Base UI Select needs a real value per item. */
const ALL = "all";

/**
 * Stable empty default: `data ?? []` allocates a new array on every render
 * while the query is loading, which is what makes a dependent memo or effect
 * fire forever (see the same constant in layout/app-sidebar.tsx).
 */
const EMPTY_SERVERS: WorkspaceMcpServer[] = [];

const SEARCH_DEBOUNCE_MS = 200;

/**
 * The workspace's MCP tool catalogue (JEF-426): one row per tool, whatever
 * server it comes from, with its risk class and how many agents reach it.
 *
 * It is a READ surface plus one shortcut. Editing a server belongs to
 * Settings, MCP; deciding what an agent may call belongs to the agent's own
 * MCP tab. Nothing here shows a url, a command, a header or an env var: the
 * API never returns them, and a page that displayed them would be the leak.
 */
export function ToolsPage() {
  const { t } = useT("tools");
  // Risk labels are the settings namespace's: the same classification is
  // named the same way on every screen that shows it.
  const { t: tSettings } = useT("settings");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();

  const [search, setSearch] = useState("");
  const [serverFilter, setServerFilter] = useState(ALL);
  const [riskFilter, setRiskFilter] = useState(ALL);
  const [activeServer, setActiveServer] = useState<WorkspaceMcpServer | null>(
    null,
  );
  const debouncedSearch = useDebouncedValue(search, SEARCH_DEBOUNCE_MS);

  const serversQuery = useQuery(workspaceMcpServersOptions(wsId));
  const servers = serversQuery.data ?? EMPTY_SERVERS;
  // One catalogue request per server. The library holds a handful of servers,
  // and each response is cached under a wsId-scoped key, so this is a small
  // fan-out and not the per-agent fan-out the agent count avoids server-side.
  const toolQueries = useQueries({
    queries: servers.map((server) =>
      workspaceMcpServerToolsOptions(wsId, server.id),
    ),
  });

  const toolsByServer = new Map<string, McpCatalogTool[]>();
  servers.forEach((server, index) => {
    const tools = toolQueries[index]?.data?.tools;
    if (tools) toolsByServer.set(server.id, tools);
  });

  const rows = buildCatalogRows(servers, toolsByServer);
  const filtered = filterCatalogRows(rows, {
    query: debouncedSearch,
    serverId: serverFilter === ALL ? "" : serverFilter,
    risk: riskFilter === ALL ? "" : riskFilter,
  });

  const loading =
    serversQuery.isLoading || toolQueries.some((query) => query.isLoading);
  // A partial catalogue is worse than an honest error: a tool that is missing
  // because its server's request failed looks exactly like a tool the agent
  // does not have.
  const failed =
    serversQuery.isError || toolQueries.some((query) => query.isError);
  const filtersActive =
    debouncedSearch.trim() !== "" || serverFilter !== ALL || riskFilter !== ALL;

  const clearFilters = () => {
    setSearch("");
    setServerFilter(ALL);
    setRiskFilter(ALL);
  };

  const retry = () => {
    void serversQuery.refetch();
    toolQueries.forEach((query) => {
      if (query.isError) void query.refetch();
    });
  };

  // Label maps for the two Selects. Not memoized: they are a handful of
  // strings, and Base UI only reads `items` to label the current value.
  const serverItems = [
    { value: ALL, label: t(($) => $.page.filter_server_all) },
    ...servers.map((server) => ({ value: server.id, label: server.name })),
  ];
  const riskLabel = (risk: McpToolRisk) =>
    tSettings(($) => $.mcp.tools[`risk_${risk}`]);
  const riskItems = [
    { value: ALL, label: t(($) => $.page.filter_risk_all) },
    ...MCP_TOOL_RISKS.map((risk) => ({ value: risk, label: riskLabel(risk) })),
  ];

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Wrench}
        title={t(($) => $.page.title)}
        count={filtered.length}
      />

      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2">
        <div className="relative min-w-40 flex-1">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            className="h-8 pl-8"
            aria-label={t(($) => $.page.search_label)}
            placeholder={t(($) => $.page.search_placeholder)}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </div>
        <Select
          items={serverItems}
          value={serverFilter}
          onValueChange={(value) => value && setServerFilter(value as string)}
        >
          <SelectTrigger size="sm" aria-label={t(($) => $.page.filter_server_label)}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {serverItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          items={riskItems}
          value={riskFilter}
          onValueChange={(value) => value && setRiskFilter(value as string)}
        >
          <SelectTrigger size="sm" aria-label={t(($) => $.page.filter_risk_label)}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {riskItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {filtersActive ? (
          <Button variant="ghost" size="sm" onClick={clearFilters}>
            {t(($) => $.page.clear_filters)}
          </Button>
        ) : null}
      </div>

      {loading ? (
        <div
          role="status"
          aria-label={t(($) => $.page.loading)}
          className="space-y-2 p-4"
        >
          {[0, 1, 2, 3, 4].map((row) => (
            <Skeleton key={row} className="h-9 w-full" />
          ))}
        </div>
      ) : failed ? (
        <CollectionPageState
          icon={Wrench}
          tone="destructive"
          role="alert"
          title={t(($) => $.page.load_error)}
          actions={
            <Button variant="outline" size="sm" onClick={retry}>
              {t(($) => $.page.retry)}
            </Button>
          }
        />
      ) : servers.length === 0 ? (
        <CollectionPageState
          icon={Wrench}
          title={t(($) => $.page.empty_servers)}
          description={t(($) => $.page.empty_servers_description)}
          actions={
            <AppLink href={`${paths.settings()}?tab=mcp`}>
              <Button variant="outline" size="sm" render={<span />}>
                {t(($) => $.page.empty_servers_action)}
              </Button>
            </AppLink>
          }
        />
      ) : rows.length === 0 ? (
        <CollectionPageState
          icon={Wrench}
          title={t(($) => $.page.empty_tools)}
          description={t(($) => $.page.empty_tools_description)}
        />
      ) : filtered.length === 0 ? (
        <CollectionPageState
          icon={Search}
          title={t(($) => $.page.empty_results)}
          description={t(($) => $.page.empty_results_description)}
          actions={
            <Button variant="outline" size="sm" onClick={clearFilters}>
              {t(($) => $.page.clear_filters)}
            </Button>
          }
        />
      ) : (
        <div className="min-h-0 flex-1 overflow-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="min-w-48">
                  {t(($) => $.page.col_tool)}
                </TableHead>
                <TableHead className="w-40">
                  {t(($) => $.page.col_server)}
                </TableHead>
                <TableHead className="w-32">
                  {t(($) => $.page.col_risk)}
                </TableHead>
                <TableHead className="w-20 text-right">
                  {t(($) => $.page.col_agents)}
                </TableHead>
                <TableHead className="w-12">
                  <span className="sr-only">{t(($) => $.page.col_actions)}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map(({ server, tool }) => (
                <TableRow key={`${server.id}:${tool.name}`}>
                  <TableCell className="max-w-96 align-top">
                    <span className="block truncate font-medium">{tool.name}</span>
                    {tool.description ? (
                      <span className="block truncate text-caption text-muted-foreground">
                        {tool.description}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell className="align-top">
                    <span className="block truncate text-caption text-muted-foreground">
                      {server.name}
                    </span>
                  </TableCell>
                  <TableCell className="align-top">
                    <Badge variant={MCP_RISK_BADGE[tool.risk] ?? "secondary"}>
                      {riskLabel(tool.risk)}
                    </Badge>
                  </TableCell>
                  <TableCell
                    className="align-top text-right font-mono tabular-nums text-muted-foreground"
                    aria-label={t(($) => $.page.agents_aria, {
                      count: server.agent_count ?? 0,
                    })}
                  >
                    {server.agent_count ?? 0}
                  </TableCell>
                  <TableCell className="align-top text-right">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={t(($) => $.page.add_to_agent_aria, {
                        server: server.name,
                      })}
                      onClick={() => setActiveServer(server)}
                    >
                      <Plus aria-hidden="true" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {activeServer ? (
        <AddToAgentDialog
          wsId={wsId}
          // Re-read from the refreshed listing so an agent attached a moment
          // ago is marked without closing the dialog.
          server={
            servers.find((server) => server.id === activeServer.id) ??
            activeServer
          }
          onClose={() => setActiveServer(null)}
        />
      ) : null}
    </div>
  );
}
