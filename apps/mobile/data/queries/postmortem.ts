/**
 * Postmortem cache key factory + query options.
 *
 * Key shape mirrors web `packages/core/postmortem/queries.ts` verbatim —
 * `["postmortem", wsId]` / `…, "stats"]` / `…, "items", state]`. The prefix
 * is singular there and stays singular here so a reader switching between
 * clients finds the same key.
 *
 * `detail` is mobile-only and has no web counterpart: web reads the selected
 * postmortem out of the already-fetched infinite pages because its detail is
 * a pane next to the list. Mobile pushes a route, which can be entered cold
 * (a `postmortem_ready` inbox notification, a process restart), so it needs
 * the real `GET /api/postmortems/{id}` endpoint behind it.
 */
import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import type { Postmortem, PostmortemState } from "@multica/core/types";
import { api } from "@/data/api";

/** Matches the server default page size (`postmortemDefaultPageSize`). */
const PAGE_SIZE = 50;

export const postmortemKeys = {
  all: (wsId: string | null) => ["postmortem", wsId] as const,
  stats: (wsId: string | null) =>
    [...postmortemKeys.all(wsId), "stats"] as const,
  items: (wsId: string | null, state: PostmortemState) =>
    [...postmortemKeys.all(wsId), "items", state] as const,
  detail: (wsId: string | null, id: string) =>
    [...postmortemKeys.all(wsId), "detail", id] as const,
};

export const postmortemStatsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: postmortemKeys.stats(wsId),
    queryFn: ({ signal }) => api.getPostmortemStats({ signal }),
    enabled: !!wsId,
  });

export const postmortemItemsOptions = (
  wsId: string | null,
  state: PostmortemState,
) =>
  infiniteQueryOptions({
    queryKey: postmortemKeys.items(wsId, state),
    queryFn: ({ pageParam, signal }) =>
      api.listPostmortems(
        { state, limit: PAGE_SIZE, cursor: pageParam ?? undefined },
        { signal },
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled: !!wsId,
  });

export const postmortemDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: postmortemKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getPostmortem(id, { signal }),
    enabled: !!wsId && !!id,
  });

export function flattenPostmortemPages(
  pages: { items: Postmortem[] }[] | undefined,
): Postmortem[] {
  return (pages ?? []).flatMap((page) => page.items);
}
