import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Linear Bridge (K21). Both reads are settings-page shaped: fetched when the
// tab opens, invalidated by the mutations, never polled.

export const linearKeys = {
  installation: (wsId: string) => ["linear-installation", wsId] as const,
  link: (wsId: string, issueId: string) => ["linear-link", wsId, issueId] as const,
};

export function linearInstallationOptions(wsId: string) {
  return queryOptions({
    queryKey: linearKeys.installation(wsId),
    queryFn: () => api.getLinearInstallation(wsId),
    enabled: !!wsId,
  });
}

export function linearLinkOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: linearKeys.link(wsId, issueId),
    queryFn: () => api.getLinearLink(wsId, issueId),
    enabled: !!wsId && !!issueId,
  });
}
