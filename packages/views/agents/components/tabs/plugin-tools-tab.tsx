"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plug } from "lucide-react";
import { toast } from "sonner";
import type { Agent, AgentPluginTool } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentPluginToolsOptions,
  useBindAgentPluginTool,
  useUnbindAgentPluginTool,
} from "@multica/core/plugins";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { AppLink } from "../../../navigation";
import { useT } from "../../../i18n";

/**
 * Deny-by-default plugin agent-tool binding. Installing a plugin into the
 * workspace grants this agent nothing on its own — a hook only becomes a
 * callable tool once its checkbox is bound here, one hook at a time.
 */
export function PluginToolsTab({ agent, canEdit }: { agent: Agent; canEdit: boolean }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const settingsHref = `${paths.settings()}?tab=plugins`;

  const toolsQuery = useQuery(agentPluginToolsOptions(wsId, agent.id));
  const bind = useBindAgentPluginTool(wsId, agent.id);
  const unbind = useUnbindAgentPluginTool(wsId, agent.id);

  const groups = useMemo(() => {
    const byPlugin = new Map<string, AgentPluginTool[]>();
    for (const tool of toolsQuery.data ?? []) {
      const list = byPlugin.get(tool.plugin_key) ?? [];
      list.push(tool);
      byPlugin.set(tool.plugin_key, list);
    }
    return Array.from(byPlugin.entries());
  }, [toolsQuery.data]);

  const isPending = bind.isPending || unbind.isPending;

  const handleToggle = (tool: AgentPluginTool, checked: boolean) => {
    const mutation = checked ? bind : unbind;
    mutation.mutate(
      { installationId: tool.installation_id, hookKey: tool.hook_key },
      { onError: () => toast.error(t(($) => $.tab_body.plugin_tools.save_failed_toast)) },
    );
  };

  return (
    <div className="space-y-4">
      <p className="text-caption text-muted-foreground">
        {t(($) => $.tab_body.plugin_tools.subtitle)}
      </p>

      {toolsQuery.isLoading ? (
        <p className="text-body text-muted-foreground">
          {t(($) => $.tab_body.plugin_tools.loading)}
        </p>
      ) : toolsQuery.isError ? (
        <p className="text-body text-destructive">
          {t(($) => $.tab_body.plugin_tools.load_failed)}
        </p>
      ) : groups.length === 0 ? (
        <div className="space-y-2 rounded-lg border border-dashed p-6 text-center">
          <p className="text-body font-medium">
            {t(($) => $.tab_body.plugin_tools.empty_title)}
          </p>
          <AppLink
            href={settingsHref}
            className="inline-flex items-center gap-1.5 text-caption font-medium text-primary hover:underline"
          >
            <Plug className="h-3 w-3" />
            {t(($) => $.tab_body.plugin_tools.empty_link_to_settings)}
          </AppLink>
        </div>
      ) : (
        <div className="space-y-4">
          {groups.map(([pluginKey, tools]) => (
            <div key={pluginKey} className="rounded-lg border">
              <p className="border-b px-3 py-2 text-caption font-medium">
                {pluginKey}
              </p>
              <ul className="divide-y">
                {tools.map((tool) => (
                  <li key={tool.hook_key} className="flex items-start gap-3 p-3">
                    <Checkbox
                      checked={tool.bound}
                      disabled={!canEdit || isPending}
                      onCheckedChange={(value) => handleToggle(tool, value === true)}
                      aria-label={t(($) => $.tab_body.plugin_tools.toggle_aria, {
                        tool: tool.name || tool.hook_key,
                      })}
                      className="mt-0.5"
                    />
                    <div className="min-w-0 flex-1 space-y-1">
                      <div className="flex items-center gap-2">
                        <p className="truncate text-body font-medium">
                          {tool.name || tool.hook_key}
                        </p>
                        <Badge variant="outline">{tool.transport}</Badge>
                      </div>
                      {tool.description && (
                        <p className="text-caption text-muted-foreground">
                          {tool.description}
                        </p>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
