"use client";

import { useQuery } from "@tanstack/react-query";
import { Blocks } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { pluginInstallationsOptions } from "@multica/core/plugins";
import { useT } from "../../i18n";

/**
 * "via <plugin>" beside a comment's author when it was posted through a
 * plugin panel. The author stays the member who used the plugin; this only
 * says which plugin wrote on their behalf. The installations list is fetched
 * only for such comments, and an uninstalled or unreadable plugin still reads
 * as "via a plugin" rather than disappearing.
 */
export function PluginViaChip({ pluginId }: { pluginId: string | null | undefined }) {
  // Hooks only run for a comment that came through a plugin.
  return pluginId ? <PluginViaLabel pluginId={pluginId} /> : null;
}

function PluginViaLabel({ pluginId }: { pluginId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: installations } = useQuery({
    ...pluginInstallationsOptions(wsId),
    enabled: wsId.length > 0,
  });
  const name = installations?.plugins?.find((i) => i.id === pluginId)?.name;
  return (
    <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
      <Blocks aria-hidden="true" className="size-3" />
      {name ? t(($) => $.comment.via_plugin, { name }) : t(($) => $.comment.via_unknown_plugin)}
    </span>
  );
}
