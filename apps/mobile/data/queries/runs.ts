/**
 * Runs fleet (OS plan, chantier 4) — cache keys + query options for
 * GET /api/runs. Cursor-paginated like postmortems
 * (`data/queries/postmortem.ts`): `infiniteQueryOptions` + a
 * `flattenRunPages` helper, same three-segment key shape
 * (`["runs", wsId, "list", state]`) as every other mobile feature.
 *
 * `state` is the server's own active|terminal|all split (not a client-side
 * filter over one big list) so the fleet screen's segmented control maps
 * 1:1 onto one query per segment — switching tabs doesn't re-filter a
 * loaded page, it fetches (or reads the cache of) that segment's own pages.
 *
 * The summary (queued/running/blocked/cost-since counts + the halt) rides
 * on every page of every segment's response — the server always recomputes
 * it fresh. The screen reads it off `pages[0]`, which is current after any
 * refetch because TanStack Query refetches every loaded page of an
 * infinite query on invalidate, in order, so page 0 always carries the
 * latest snapshot once a refetch (WS-triggered or pull-to-refresh)
 * completes.
 */
import { infiniteQueryOptions } from "@tanstack/react-query";
import type { RunsResponse } from "@/data/schemas";
import { api } from "@/data/api";

export type RunsFilterState = "active" | "terminal" | "all";

const PAGE_SIZE = 50;

export const runsKeys = {
  all: (wsId: string | null) => ["runs", wsId] as const,
  list: (wsId: string | null, state: RunsFilterState) =>
    [...runsKeys.all(wsId), "list", state] as const,
};

export function runsListOptions(wsId: string | null, state: RunsFilterState) {
  return infiniteQueryOptions({
    queryKey: runsKeys.list(wsId, state),
    queryFn: ({ pageParam, signal }) =>
      api.listRuns({ state, cursor: pageParam, limit: PAGE_SIZE }, { signal }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor || undefined,
    enabled: !!wsId,
  });
}

export function flattenRunPages(
  pages: RunsResponse[] | undefined,
): RunsResponse["runs"] {
  return (pages ?? []).flatMap((page) => page.runs);
}
