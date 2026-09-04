import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { PostmortemState } from "../types";

export const postmortemKeys = {
  all: (wsId: string) => ["postmortem", wsId] as const,
  stats: (wsId: string) => [...postmortemKeys.all(wsId), "stats"] as const,
  items: (wsId: string, state: PostmortemState) =>
    [...postmortemKeys.all(wsId), "items", state] as const,
};

/**
 * Per-state counts that drive the nav badge and the page's filter chips. One
 * entry per workspace.
 */
export function postmortemStatsOptions(wsId: string) {
  return queryOptions({
    queryKey: postmortemKeys.stats(wsId),
    queryFn: ({ signal }) => api.getPostmortemStats({ signal }),
  });
}

/**
 * One review state (default draft), newest first. Keyset-paginated; the page
 * fetches the first page and the server returns `next_cursor`.
 */
export function postmortemItemsOptions(wsId: string, state: PostmortemState) {
  return queryOptions({
    queryKey: postmortemKeys.items(wsId, state),
    queryFn: ({ signal }) => api.listPostmortems({ state }, { signal }),
  });
}
