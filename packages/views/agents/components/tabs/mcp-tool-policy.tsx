"use client";

import { useState } from "react";
import { ChevronDown, Loader2 } from "lucide-react";
import { toast } from "sonner";
import type {
  McpCatalogTool,
  McpToolClass,
  McpToolPolicy as McpToolPolicyValue,
  McpToolRisk,
  WorkspaceMcpServer,
} from "@multica/core/types";
import { useSetAgentMcpServerPolicy } from "@multica/core/workspace/mutations";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { useT, useTimeAgo } from "../../../i18n";

const POLICY_DEFAULTS = ["by_risk", "ask", "never"] as const;
const TOOL_CLASSES: McpToolClass[] = ["act_alone", "ask", "never"];

const SELECT_CLASS =
  "h-8 rounded-md border border-input bg-transparent px-2 text-caption";

const RISK_BADGE: Record<McpToolRisk, "outline" | "secondary" | "destructive"> = {
  read: "outline",
  internal_write: "secondary",
  external_effect: "destructive",
  sensitive_data: "destructive",
  unknown: "secondary",
};

/**
 * The class a tool gets under a policy, mirroring the gateway's rule: an
 * explicit entry wins, otherwise `by_risk` lets reads and internal writes act
 * alone and asks for everything else. The server also applies the trust dial
 * ceiling, which this cannot see — so the saved value from the server is
 * preferred whenever the draft is untouched.
 */
export function effectiveToolClass(
  policy: McpToolPolicyValue,
  tool: McpCatalogTool,
): McpToolClass {
  const explicit = policy.tools?.[tool.name];
  if (explicit) return explicit;
  switch (policy.default ?? "by_risk") {
    case "ask":
      return "ask";
    case "never":
      return "never";
    default:
      return tool.risk === "read" || tool.risk === "internal_write"
        ? "act_alone"
        : "ask";
  }
}

/**
 * Per-tool approval classes of one workspace server bound to an agent (K77).
 * Edits accumulate in a draft and are saved as one policy; a 400 from the
 * gateway (trust dial ceiling, Rule of Two) is shown under the editor, where
 * the offending select is.
 */
export function McpToolPolicy({
  agentId,
  server,
  canEdit,
}: {
  agentId: string;
  server: WorkspaceMcpServer;
  canEdit: boolean;
}) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const setPolicy = useSetAgentMcpServerPolicy(agentId);
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<McpToolPolicyValue | null>(null);
  const [error, setError] = useState<string | null>(null);

  const saved: McpToolPolicyValue = server.tool_policy ?? {};
  const policy = draft ?? saved;
  const tools = server.tools ?? [];

  const setToolClass = (name: string, value: string) => {
    const next = { ...policy.tools };
    if (value === "") delete next[name];
    else next[name] = value as McpToolClass;
    setDraft({ ...policy, tools: next });
  };

  const handleSave = async () => {
    if (!draft) return;
    setError(null);
    try {
      await setPolicy.mutateAsync({ serverId: server.id, policy: draft });
      setDraft(null);
      toast.success(t(($) => $.tab_body.mcp_config.policy.saved_toast));
    } catch (err) {
      setError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.mcp_config.workspace_action_failed),
      );
    }
  };

  const classLabel = (value: McpToolClass) =>
    t(($) => $.tab_body.mcp_config.policy[`class_${value}`]);

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="px-3 pb-3">
      <CollapsibleTrigger
        render={
          <Button variant="ghost" size="sm">
            {t(($) => $.tab_body.mcp_config.policy.toggle)}
            <ChevronDown
              className={open ? "h-4 w-4 rotate-180" : "h-4 w-4"}
              aria-hidden="true"
            />
          </Button>
        }
      />
      <CollapsibleContent className="space-y-3 pt-2">
        <p className="text-caption leading-5 text-muted-foreground">
          {t(($) => $.tab_body.mcp_config.policy.help)}
        </p>
        <label className="flex items-center gap-2 text-caption">
          {t(($) => $.tab_body.mcp_config.policy.default_label)}
          <select
            className={SELECT_CLASS}
            value={policy.default ?? "by_risk"}
            disabled={!canEdit}
            onChange={(event) =>
              setDraft({
                ...policy,
                default: event.target.value as McpToolPolicyValue["default"],
              })
            }
          >
            {POLICY_DEFAULTS.map((value) => (
              <option key={value} value={value}>
                {t(($) => $.tab_body.mcp_config.policy[`default_${value}`])}
              </option>
            ))}
          </select>
        </label>

        {tools.length === 0 ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.tab_body.mcp_config.policy.empty)}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-caption">
              <thead className="text-muted-foreground">
                <tr className="text-left">
                  <th className="py-1 pr-3 font-medium">
                    {t(($) => $.tab_body.mcp_config.policy.col_tool)}
                  </th>
                  <th className="py-1 pr-3 font-medium">
                    {t(($) => $.tab_body.mcp_config.policy.col_risk)}
                  </th>
                  <th className="py-1 pr-3 font-medium">
                    {t(($) => $.tab_body.mcp_config.policy.col_effective)}
                  </th>
                  <th className="py-1 pr-3 font-medium">
                    {t(($) => $.tab_body.mcp_config.policy.col_class)}
                  </th>
                  <th className="py-1 font-medium">
                    {t(($) => $.tab_body.mcp_config.policy.col_last_used)}
                  </th>
                </tr>
              </thead>
              <tbody>
                {tools.map((tool) => {
                  const effective =
                    draft === null && tool.class
                      ? tool.class
                      : effectiveToolClass(policy, tool);
                  return (
                    <tr key={tool.name} className="border-t border-surface-border">
                      <td className="max-w-64 py-1.5 pr-3">
                        <p className="truncate font-medium">{tool.name}</p>
                        {tool.description ? (
                          <p className="truncate text-muted-foreground">
                            {tool.description}
                          </p>
                        ) : null}
                      </td>
                      <td className="py-1.5 pr-3">
                        <Badge variant={RISK_BADGE[tool.risk] ?? "secondary"}>
                          {t(($) => $.tab_body.mcp_config.policy[`risk_${tool.risk}`])}
                        </Badge>
                      </td>
                      <td className="py-1.5 pr-3">{classLabel(effective)}</td>
                      <td className="py-1.5 pr-3">
                        <select
                          aria-label={t(($) => $.tab_body.mcp_config.policy.class_aria, {
                            name: tool.name,
                          })}
                          className={SELECT_CLASS}
                          value={policy.tools?.[tool.name] ?? ""}
                          disabled={!canEdit}
                          onChange={(event) => setToolClass(tool.name, event.target.value)}
                        >
                          <option value="">
                            {t(($) => $.tab_body.mcp_config.policy.class_inherit)}
                          </option>
                          {TOOL_CLASSES.map((value) => (
                            <option key={value} value={value}>
                              {classLabel(value)}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td className="py-1.5 text-muted-foreground">
                        {tool.last_used_at ? timeAgo(tool.last_used_at) : "—"}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {error ? (
          <p role="alert" className="text-caption text-destructive">
            {error}
          </p>
        ) : null}

        {canEdit ? (
          <Button
            size="sm"
            disabled={draft === null || setPolicy.isPending}
            onClick={() => void handleSave()}
          >
            {setPolicy.isPending ? (
              <Loader2 className="animate-spin" aria-hidden="true" />
            ) : null}
            {t(($) => $.tab_body.mcp_config.policy.save)}
          </Button>
        ) : null}
      </CollapsibleContent>
    </Collapsible>
  );
}
