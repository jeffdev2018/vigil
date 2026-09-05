/**
 * Postmortem cache key factory + query options.
 *
 * Key shape mirrors web `packages/core/postmortem/queries.ts` —
 * `["postmortems", wsId, "list", state]` / `["postmortems", wsId, "stats"]` /
 * `["postmortems", wsId, "detail", id]`.
 *
 * Unlike triage, postmortems DO have a detail endpoint
 * (`GET /api/postmortems/{id}`), so the detail screen owns a real query and
 * survives a cold deep link from an inbox `postmortem_ready` notification.
 */
import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import type { Postmortem, PostmortemState } from "@multica/core/types";
import { api } from "@/data/api";

const PAGE_SIZE = 50;

export const postmortemKeys = {
  all: (wsId: string | null) => ["postmortems", wsId] as const,
  stats: (wsId: string | null) =>
    [...postmortemKeys.all(wsId), "stats"] as const,
  list: (wsId: string | null, state: PostmortemState) =>
    [...postmortemKeys.all(wsId), "list", state] as const,
  detail: (wsId: string | null, id: string) =>
    [...postmortemKeys.all(wsId), "detail", id] as const,
};

export const postmortemStatsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: postmortemKeys.stats(wsId),
    queryFn: ({ signal }) => api.getPostmortemStats({ signal }),
    enabled: !!wsId,
  });

export const postmortemListOptions = (
  wsId: string | null,
  state: PostmortemState,
) =>
  infiniteQueryOptions({
    queryKey: postmortemKeys.list(wsId, state),
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
