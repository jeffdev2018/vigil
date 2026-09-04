import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { TriageItemState } from "../types";

export const triageKeys = {
  all: (wsId: string) => ["triage", wsId] as const,
  stats: (wsId: string) => [...triageKeys.all(wsId), "stats"] as const,
  items: (wsId: string, state: TriageItemState) =>
    [...triageKeys.all(wsId), "items", state] as const,
};

/**
 * Queue volume + the source list that drives the header strip and the
 * per-source mode switch. One entry per workspace.
 */
export function triageStatsOptions(wsId: string) {
  return queryOptions({
    queryKey: triageKeys.stats(wsId),
    queryFn: ({ signal }) => api.getTriageStats({ signal }),
  });
}

/**
 * One visible queue state (default pending), newest first. Keyset-paginated;
 * the page fetches the first page and the server returns `next_cursor`.
 * Shadow measurement rows never appear here — the server filters them.
 */
export function triageItemsOptions(wsId: string, state: TriageItemState) {
  return queryOptions({
    queryKey: triageKeys.items(wsId, state),
    queryFn: ({ signal }) => api.listTriageItems({ state }, { signal }),
  });
}
