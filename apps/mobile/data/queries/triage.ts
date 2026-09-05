/**
 * Triage cache key factory + query options.
 *
 * Key shape mirrors web `packages/core/triage/queries.ts` —
 * `["triage", wsId, "items", state]` / `["triage", wsId, "stats"]` — so a
 * reader switching between mobile and web finds the same prefixes, and
 * `invalidateQueries({ queryKey: triageKeys.all(wsId) })` clears the whole
 * feature for a workspace in one call.
 *
 * Items use an infinite query because the backend paginates by keyset
 * (`next_cursor`), not by offset: `GET /api/triage/items?state=&cursor=`.
 * The list screen appends pages on `onEndReached`; the detail screen reads
 * the already-fetched item out of this cache (there is no
 * `GET /api/triage/items/{id}` endpoint on the server).
 */
import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import type { TriageItem, TriageItemState } from "@multica/core/types";
import { api } from "@/data/api";

/** Page size. Matches the server default (`triageDefaultPageSize`). */
const PAGE_SIZE = 50;

export const triageKeys = {
  all: (wsId: string | null) => ["triage", wsId] as const,
  stats: (wsId: string | null) => [...triageKeys.all(wsId), "stats"] as const,
  items: (wsId: string | null, state: TriageItemState) =>
    [...triageKeys.all(wsId), "items", state] as const,
};

export const triageStatsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: triageKeys.stats(wsId),
    queryFn: ({ signal }) => api.getTriageStats({ signal }),
    enabled: !!wsId,
  });

export const triageItemsOptions = (
  wsId: string | null,
  state: TriageItemState,
) =>
  infiniteQueryOptions({
    queryKey: triageKeys.items(wsId, state),
    queryFn: ({ pageParam, signal }) =>
      api.listTriageItems(
        { state, limit: PAGE_SIZE, cursor: pageParam ?? undefined },
        { signal },
      ),
    initialPageParam: undefined as string | undefined,
    // `next_cursor` is absent on the last page — returning undefined is what
    // flips `hasNextPage` to false.
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled: !!wsId,
  });

/**
 * Flattens every fetched page into one array. Kept here (not in the screen)
 * so the list and the detail screen agree on ordering: the server already
 * returns newest-first per page, and pages append in fetch order.
 */
export function flattenTriagePages(
  pages: { items: TriageItem[] }[] | undefined,
): TriageItem[] {
  return (pages ?? []).flatMap((page) => page.items);
}
