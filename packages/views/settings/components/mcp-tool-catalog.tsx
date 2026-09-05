"use client";

import { useState } from "react";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import {
  workspaceKeys,
  workspaceMcpServerToolsOptions,
} from "@multica/core/workspace/queries";
import type { McpCatalogTool, McpToolRisk } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

export const MCP_TOOL_RISKS: McpToolRisk[] = [
  "read",
  "internal_write",
  "external_effect",
  "sensitive_data",
  "unknown",
];

const SELECT_CLASS =
  "h-8 rounded-md border border-input bg-transparent px-2 text-caption";

/**
 * The tool catalogue of one workspace MCP server (K77): what the server
 * exposes, each tool classified by risk. The whole list is saved at once —
 * the API replaces it — so edits accumulate in a local draft until Save.
 * Discovery asks the server itself (HTTP transports); a stdio server is
 * catalogued by the daemon at its first run, and the API says so with a 400
 * that is shown inline rather than only toasted.
 */
export function McpToolCatalog({
  wsId,
  serverId,
  canManage,
}: {
  wsId: string;
  serverId: string;
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const queryClient = useQueryClient();
  const catalogQuery = useQuery(workspaceMcpServerToolsOptions(wsId, serverId));
  const [draft, setDraft] = useState<McpCatalogTool[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"discover" | "save" | null>(null);
  const [newName, setNewName] = useState("");
  const [newDescription, setNewDescription] = useState("");
  const [newRisk, setNewRisk] = useState<McpToolRisk>("unknown");

  const tools = draft ?? catalogQuery.data?.tools ?? [];

  const invalidate = () => {
    void queryClient.invalidateQueries({
      queryKey: workspaceKeys.mcpServerTools(wsId, serverId),
    });
    void queryClient.invalidateQueries({ queryKey: workspaceKeys.mcpServers(wsId) });
  };

  const messageOf = (err: unknown, fallback: string) =>
    err instanceof Error && err.message ? err.message : fallback;

  const handleDiscover = async () => {
    setBusy("discover");
    setError(null);
    try {
      await api.discoverWorkspaceMcpServerTools(wsId, serverId);
      setDraft(null);
      invalidate();
      toast.success(t(($) => $.mcp.tools.discovered_toast));
    } catch (err) {
      setError(messageOf(err, t(($) => $.mcp.tools.save_failed_toast)));
    } finally {
      setBusy(null);
    }
  };

  const handleSave = async () => {
    if (!draft) return;
    setBusy("save");
    setError(null);
    try {
      await api.setWorkspaceMcpServerTools(
        wsId,
        serverId,
        draft.map(({ name, description, risk }) => ({ name, description, risk })),
      );
      setDraft(null);
      invalidate();
      toast.success(t(($) => $.mcp.tools.saved_toast));
    } catch (err) {
      setError(messageOf(err, t(($) => $.mcp.tools.save_failed_toast)));
    } finally {
      setBusy(null);
    }
  };

  const setRisk = (name: string, risk: McpToolRisk) =>
    setDraft(
      tools.map((tool) =>
        tool.name === name ? { ...tool, risk, risk_source: "manual" } : tool,
      ),
    );

  const removeTool = (name: string) =>
    setDraft(tools.filter((tool) => tool.name !== name));

  const addTool = () => {
    const name = newName.trim();
    if (!name || tools.some((tool) => tool.name === name)) return;
    const description = newDescription.trim();
    setDraft([
      ...tools,
      {
        name,
        ...(description ? { description } : {}),
        risk: newRisk,
        risk_source: "manual",
      },
    ]);
    setNewName("");
    setNewDescription("");
    setNewRisk("unknown");
  };

  const riskLabel = (risk: McpToolRisk) => t(($) => $.mcp.tools[`risk_${risk}`]);

  return (
    <div className="space-y-3 border-t border-surface-border px-4 py-3">
      <p className="text-caption leading-5 text-muted-foreground">
        {t(($) => $.mcp.tools.help)}
      </p>

      {catalogQuery.isLoading ? (
        <p className="flex items-center gap-2 text-caption text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
          {t(($) => $.mcp.tools.loading)}
        </p>
      ) : tools.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.mcp.tools.empty)}
        </p>
      ) : (
        <ul className="divide-y divide-surface-border rounded-md border border-surface-border">
          {tools.map((tool) => (
            <li key={tool.name} className="flex items-center gap-3 px-3 py-2">
              <div className="min-w-0 flex-1">
                <p className="truncate text-body font-medium">{tool.name}</p>
                {tool.description ? (
                  <p className="truncate text-caption text-muted-foreground">
                    {tool.description}
                  </p>
                ) : null}
              </div>
              <span className="shrink-0 text-caption text-muted-foreground">
                {tool.risk_source === "manual"
                  ? t(($) => $.mcp.tools.source_manual)
                  : t(($) => $.mcp.tools.source_auto)}
              </span>
              <select
                aria-label={t(($) => $.mcp.tools.risk_aria, { name: tool.name })}
                className={SELECT_CLASS}
                value={tool.risk}
                disabled={!canManage}
                onChange={(event) => setRisk(tool.name, event.target.value as McpToolRisk)}
              >
                {MCP_TOOL_RISKS.map((risk) => (
                  <option key={risk} value={risk}>
                    {riskLabel(risk)}
                  </option>
                ))}
              </select>
              {canManage ? (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.mcp.tools.remove_aria, { name: tool.name })}
                  onClick={() => removeTool(tool.name)}
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      )}

      {canManage ? (
        <>
          <form
            className="flex flex-wrap items-center gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              addTool();
            }}
          >
            <Input
              className="h-8 w-40"
              aria-label={t(($) => $.mcp.tools.name_placeholder)}
              placeholder={t(($) => $.mcp.tools.name_placeholder)}
              value={newName}
              onChange={(event) => setNewName(event.target.value)}
            />
            <Input
              className="h-8 min-w-0 flex-1"
              aria-label={t(($) => $.mcp.tools.description_placeholder)}
              placeholder={t(($) => $.mcp.tools.description_placeholder)}
              value={newDescription}
              onChange={(event) => setNewDescription(event.target.value)}
            />
            <select
              aria-label={t(($) => $.mcp.tools.risk_aria, { name: newName || "?" })}
              className={SELECT_CLASS}
              value={newRisk}
              onChange={(event) => setNewRisk(event.target.value as McpToolRisk)}
            >
              {MCP_TOOL_RISKS.map((risk) => (
                <option key={risk} value={risk}>
                  {riskLabel(risk)}
                </option>
              ))}
            </select>
            <Button type="submit" size="sm" variant="outline" disabled={!newName.trim()}>
              <Plus aria-hidden="true" />
              {t(($) => $.mcp.tools.add)}
            </Button>
          </form>

          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={busy !== null}
              onClick={() => void handleDiscover()}
            >
              {busy === "discover" ? (
                <Loader2 className="animate-spin" aria-hidden="true" />
              ) : null}
              {t(($) => $.mcp.tools.discover)}
            </Button>
            <Button
              size="sm"
              disabled={draft === null || busy !== null}
              onClick={() => void handleSave()}
            >
              {busy === "save" ? (
                <Loader2 className="animate-spin" aria-hidden="true" />
              ) : null}
              {t(($) => $.mcp.tools.save)}
            </Button>
          </div>
        </>
      ) : null}

      {error ? (
        <p role="alert" className="text-caption text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  );
}
