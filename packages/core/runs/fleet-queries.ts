import { infiniteQueryOptions } from "@tanstack/react-query";
import { api } from "../api";

export interface RunsFilter {
  state?: "active" | "terminal" | "all";
  status?: string;
  agentId?: string;
  issueId?: string;
  runtimeId?: string;
  since?: string;
}

function filterKeyPart(filter: RunsFilter) {
  return [
    filter.state ?? "all",
    filter.status ?? "",
    filter.agentId ?? "",
    filter.issueId ?? "",
    filter.runtimeId ?? "",
    filter.since ?? "",
  ] as const;
}

export const runKeys = {
  all: (wsId: string) => ["runs", wsId] as const,
  list: (wsId: string, filter: RunsFilter) =>
    [...runKeys.all(wsId), "list", ...filterKeyPart(filter)] as const,
};

/**
 * The fleet list, keyset-paginated: the server returns `next_cursor` while
 * more rows exist, walked with a Load more button. Each page also carries
 * the workspace summary (queued/running/blocked/cost) recomputed fresh —
 * the header reads the most recently fetched page's summary.
 */
export function workspaceRunsInfiniteOptions(wsId: string, filter: RunsFilter = {}) {
  return infiniteQueryOptions({
    queryKey: runKeys.list(wsId, filter),
    queryFn: ({ pageParam, signal }) =>
      api.listRuns(
        {
          state: filter.state,
          status: filter.status,
          agentId: filter.agentId,
          issueId: filter.issueId,
          runtimeId: filter.runtimeId,
          since: filter.since,
          cursor: pageParam || undefined,
        },
        { signal },
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
    enabled: !!wsId,
    // Realtime task:* / run_halt:changed events invalidate this key; the
    // short interval is only the reconnect / missed-event safety net.
    refetchInterval: 30_000,
  });
}
