import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { linearKeys } from "./queries";

/**
 * Connecting is a navigation, not a cache change: the server answers with the
 * Linear authorize URL and the browser leaves. The installation row only
 * exists after Linear redirects back, so there is nothing to invalidate here.
 */
export function useStartLinearOAuth(wsId: string) {
  return useMutation({
    mutationFn: (input: { agentId: string; redirect?: string }) =>
      api.startLinearOAuth(wsId, input.agentId, input.redirect),
  });
}

export function useDisconnectLinear(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.disconnectLinear(wsId),
    onSettled: () => qc.invalidateQueries({ queryKey: linearKeys.installation(wsId) }),
  });
}

export function useUpdateLinearStatusMap(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (statusMap: Record<string, string>) => api.updateLinearStatusMap(wsId, statusMap),
    onSettled: () => qc.invalidateQueries({ queryKey: linearKeys.installation(wsId) }),
  });
}

/**
 * Resync re-reads the Linear issue and reconciles the mirror. It also clears a
 * `broken` link, so both the badge and the settings banner can change.
 */
export function useResyncLinearLink(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (linkId: string) => api.resyncLinearLink(wsId, linkId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: linearKeys.link(wsId, issueId) });
      qc.invalidateQueries({ queryKey: linearKeys.installation(wsId) });
    },
  });
}
