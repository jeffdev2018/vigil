"use client";

import { useEffect, useState } from "react";
import { Check, Copy, Server } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { api } from "@multica/core/api";
import {
  mcpServerSettingsOptions,
  useUpdateMCPServerSettings,
  type MCPServerSurface,
  type MCPToolDecision,
} from "@multica/core/agents/mcp-server";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { copyText } from "@multica/ui/lib/clipboard";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

const SELECT_CLASS =
  "h-8 rounded-md border border-input bg-transparent px-2 text-caption";

const RISK_BADGE: Record<string, "outline" | "secondary" | "destructive"> = {
  read: "outline",
  internal_write: "secondary",
  external_effect: "destructive",
  sensitive_data: "destructive",
  unknown: "secondary",
};

const KNOWN_RISKS = ["read", "internal_write", "external_effect", "sensitive_data", "unknown"] as const;

/**
 * MCP server (OS plan, chantier 1). This workspace exposed as one MCP
 * server: on/off, the default tool surface, and per-tool overrides that can
 * only tighten what a caller's own ceiling already grants — never loosen
 * it. Read-only for anyone who isn't an owner or admin, matching the
 * backend (GET is any member, PUT is owner/admin only).
 */
export function MCPServerSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data } = useQuery(mcpServerSettingsOptions(wsId));
  const save = useUpdateMCPServerSettings(wsId);

  const [enabled, setEnabled] = useState(true);
  const [surface, setSurface] = useState<MCPServerSurface>("compound");
  const [overrides, setOverrides] = useState<Record<string, MCPToolDecision>>({});
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!data) return;
    setEnabled(data.settings.enabled);
    setSurface(data.settings.default_surface);
    setOverrides(data.settings.tools);
  }, [data]);

  const dirty =
    !!data &&
    (enabled !== data.settings.enabled ||
      surface !== data.settings.default_surface ||
      JSON.stringify(overrides) !== JSON.stringify(data.settings.tools));

  const setOverride = (name: string, value: string) => {
    setOverrides((prev) => {
      const next = { ...prev };
      if (value === "") delete next[name];
      else next[name] = value as MCPToolDecision;
      return next;
    });
  };

  const handleSave = () => {
    save.mutate(
      { enabled, default_surface: surface, tools: overrides },
      {
        onSuccess: () => toast.success(t(($) => $.mcp_server.saved_toast)),
        onError: (e: unknown) =>
          toast.error(e instanceof Error && e.message ? e.message : t(($) => $.mcp_server.failed_toast)),
      },
    );
  };

  const endpointUrl = data ? `${api.getBaseUrl()}${data.endpoint}` : "";
  const handleCopyEndpoint = async () => {
    if (!endpointUrl) return;
    if (await copyText(endpointUrl)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } else {
      toast.error(t(($) => $.mcp_server.copy_failed));
    }
  };

  const snippet = JSON.stringify(
    {
      mcpServers: {
        vigil: {
          type: "http",
          url: endpointUrl || "…",
          headers: { Authorization: "Bearer <your personal access token>" },
        },
      },
    },
    null,
    2,
  );
  const [snippetCopied, setSnippetCopied] = useState(false);
  const handleCopySnippet = async () => {
    if (await copyText(snippet)) {
      setSnippetCopied(true);
      setTimeout(() => setSnippetCopied(false), 2000);
    } else {
      toast.error(t(($) => $.mcp_server.copy_failed));
    }
  };

  const tools = data?.tools ?? [];
  const groups = [...new Set(tools.map((tool) => tool.group))];

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Server className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.mcp_server.section)}
        </span>
      }
      description={t(($) => $.mcp_server.intro)}
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.mcp_server.endpoint_label)} size="text" align="start">
          <div className="flex items-center gap-2">
            <Input
              readOnly
              aria-label={t(($) => $.mcp_server.endpoint_label)}
              value={endpointUrl}
              className="min-w-0 font-mono text-caption"
            />
            <Button variant="outline" size="sm" className="shrink-0" onClick={() => void handleCopyEndpoint()} title={t(($) => $.mcp_server.copy)}>
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            </Button>
          </div>
        </SettingsRow>

        <SettingsRow label={t(($) => $.mcp_server.enabled_label)} description={t(($) => $.mcp_server.enabled_description)}>
          <Switch
            aria-label={t(($) => $.mcp_server.enabled_label)}
            checked={enabled}
            disabled={!canEdit || save.isPending}
            onCheckedChange={(checked: boolean) => setEnabled(checked)}
          />
        </SettingsRow>

        <SettingsRow label={t(($) => $.mcp_server.surface_label)} description={t(($) => $.mcp_server.surface_description)} align="start">
          <fieldset className="flex flex-col gap-2" disabled={!canEdit || save.isPending}>
            <legend className="sr-only">{t(($) => $.mcp_server.surface_label)}</legend>
            {(["compound", "granular"] as const).map((value) => (
              <label key={value} className="flex items-start gap-2 text-caption">
                <input
                  type="radio"
                  name="mcp-server-surface"
                  className="mt-0.5"
                  value={value}
                  aria-label={value === "compound" ? t(($) => $.mcp_server.surface_compound) : t(($) => $.mcp_server.surface_granular)}
                  checked={surface === value}
                  onChange={() => setSurface(value)}
                />
                <span>
                  <span className="font-medium">
                    {value === "compound" ? t(($) => $.mcp_server.surface_compound) : t(($) => $.mcp_server.surface_granular)}
                  </span>
                  <span className="block text-muted-foreground">
                    {value === "compound"
                      ? t(($) => $.mcp_server.surface_compound_description)
                      : t(($) => $.mcp_server.surface_granular_description)}
                  </span>
                </span>
              </label>
            ))}
          </fieldset>
        </SettingsRow>
      </SettingsCard>

      <div className="rounded-md border bg-background">
        <div className="flex items-center justify-between px-3 py-2 text-caption">
          <span className="font-medium">{t(($) => $.mcp_server.snippet_title)}</span>
          <button
            type="button"
            onClick={() => void handleCopySnippet()}
            className="flex items-center gap-1 rounded px-2 py-0.5 text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
          >
            {snippetCopied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
            {snippetCopied ? t(($) => $.mcp_server.copied) : t(($) => $.mcp_server.copy)}
          </button>
        </div>
        <pre className="overflow-auto border-t bg-muted/40 px-3 py-2 text-caption font-mono leading-relaxed">{snippet}</pre>
        <p className="border-t px-3 py-2 text-caption text-muted-foreground">
          {t(($) => $.mcp_server.pat_hint)}{" "}
          <AppLink href={`${paths.settings()}?tab=tokens`} className="underline underline-offset-2 hover:text-foreground">
            {t(($) => $.mcp_server.pat_link)}
          </AppLink>
        </p>
      </div>

      <div className="px-0.5">
        <h4 className="text-body font-semibold">{t(($) => $.mcp_server.tools_section)}</h4>
        <p className="mt-1 text-caption leading-5 text-muted-foreground">{t(($) => $.mcp_server.tools_description)}</p>
      </div>
      <SettingsCard>
        {groups.map((group) => (
            <div key={group} className="px-4 py-3">
              <p className="mb-2 font-mono text-caption text-muted-foreground">{group}</p>
              <div className="overflow-x-auto">
                <table className="w-full text-caption">
                  <thead className="text-muted-foreground">
                    <tr className="text-left">
                      <th className="py-1 pr-3 font-medium">{t(($) => $.mcp_server.col_tool)}</th>
                      <th className="py-1 pr-3 font-medium">{t(($) => $.mcp_server.col_risk)}</th>
                      <th className="py-1 pr-3 font-medium">{t(($) => $.mcp_server.col_description)}</th>
                      <th className="py-1 font-medium">{t(($) => $.mcp_server.col_override)}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tools
                      .filter((tool) => tool.group === group)
                      .map((tool) => {
                        const risk = KNOWN_RISKS.includes(tool.risk as (typeof KNOWN_RISKS)[number]) ? tool.risk : "unknown";
                        return (
                          <tr key={tool.name} className="border-t border-surface-border align-top">
                            <td className="max-w-56 py-1.5 pr-3">
                              <p className="truncate font-mono font-medium">{tool.name}</p>
                              {tool.agent_only && (
                                <Badge variant="outline" className="mt-1">
                                  {t(($) => $.mcp_server.agent_only)}
                                </Badge>
                              )}
                            </td>
                            <td className="py-1.5 pr-3">
                              <Badge variant={RISK_BADGE[risk] ?? "secondary"}>
                                {t(($) => $.mcp_server[`risk_${risk as (typeof KNOWN_RISKS)[number]}`])}
                              </Badge>
                            </td>
                            <td className="max-w-96 py-1.5 pr-3 text-muted-foreground">{tool.description}</td>
                            <td className="py-1.5">
                              <select
                                aria-label={t(($) => $.mcp_server.override_aria, { name: tool.name })}
                                className={SELECT_CLASS}
                                value={overrides[tool.name] ?? ""}
                                disabled={!canEdit || save.isPending}
                                onChange={(event) => setOverride(tool.name, event.target.value)}
                              >
                                <option value="">{t(($) => $.mcp_server.decision_default)}</option>
                                <option value="allow">{t(($) => $.mcp_server.decision_allow)}</option>
                                <option value="ask">{t(($) => $.mcp_server.decision_ask)}</option>
                                <option value="deny">{t(($) => $.mcp_server.decision_deny)}</option>
                              </select>
                            </td>
                          </tr>
                        );
                      })}
                  </tbody>
                </table>
              </div>
            </div>
          ))}
      </SettingsCard>

      {!canEdit && <p className="px-0.5 text-caption text-muted-foreground">{t(($) => $.mcp_server.readonly_note)}</p>}

      {canEdit && (
        <Button size="sm" disabled={!dirty || save.isPending} onClick={handleSave}>
          {save.isPending ? t(($) => $.mcp_server.saving) : t(($) => $.mcp_server.save)}
        </Button>
      )}
    </SettingsSection>
  );
}
