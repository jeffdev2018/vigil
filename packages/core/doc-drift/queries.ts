import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { DocDriftSettings } from "./schemas";

// Agent context drift detection (K56). Both reads poll while a scan is in
// flight (same 10s cadence as the K22 code health history) so the proposals
// land without a websocket subscription.

const POLL_MS = 10_000;

export const docDriftKeys = {
  settings: (wsId: string) => ["doc-drift-settings", wsId] as const,
  proposals: (wsId: string) => ["doc-drift-proposals", wsId] as const,
};

export function docDriftSettingsOptions(wsId: string) {
  return queryOptions({
    queryKey: docDriftKeys.settings(wsId),
    queryFn: () => api.getDocDriftSettings(),
    enabled: !!wsId,
    refetchInterval: (query) =>
      (query.state.data as DocDriftSettings | undefined)?.repos?.some((repo) => repo.scanning)
        ? POLL_MS
        : false,
  });
}

export function docDriftProposalsOptions(wsId: string) {
  return queryOptions({
    queryKey: docDriftKeys.proposals(wsId),
    queryFn: () => api.listDocDriftProposals(),
    enabled: !!wsId,
  });
}
