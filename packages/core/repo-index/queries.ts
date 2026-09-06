import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Shared semantic repo index (K47). The settings read is static: the index is
// refreshed by the daemon after runs, so there is no in-flight state worth
// polling for — a user who wants fresher numbers reopens the tab.

export const repoIndexKeys = {
  settings: (wsId: string) => ["repo-index-settings", wsId] as const,
};

export function repoIndexSettingsOptions(wsId: string) {
  return queryOptions({
    queryKey: repoIndexKeys.settings(wsId),
    queryFn: () => api.getRepoIndexSettings(),
    enabled: !!wsId,
  });
}
