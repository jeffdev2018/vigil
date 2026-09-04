import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { TriageItemState } from "../types";

export const triageKeys = {
  all: (wsId: string) => ["triage", wsId] as const,
  stats: (wsId: string) => [...triageKeys.all(wsId), "stats"] as const,
  items: (wsId: string, state: TriageItemState) =>
    [...triageKeys.all(wsId), "items", state] as const,
  suggestions: (wsId: string, ids: string[]) =>
    [...triageKeys.all(wsId), "suggestions", ids.join(",")] as const,
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

/**
 * Triage auto-ML (K61): suggestions for the visible items, one request per
 * page of ids. Empty ids fetch nothing.
 */
export function triageSuggestionsOptions(wsId: string, ids: string[]) {
  const sorted = [...ids].sort().slice(0, 50);
  return queryOptions({
    queryKey: triageKeys.suggestions(wsId, sorted),
    queryFn: ({ signal }) => api.getTriageSuggestions(sorted, { signal }),
    enabled: sorted.length > 0,
    staleTime: 30_000,
  });
}
