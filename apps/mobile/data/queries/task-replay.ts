/**
 * Run replay cache key factory + query options.
 *
 * Mobile-only: web's scrubber (built in parallel) owns its own key. Shape
 * follows the 3-segment convention — `["task-replay", wsId, taskId]`.
 *
 * The endpoint is paginated by `next_cursor`; the query follows every page
 * so the scrubber can jump to any position without a network round trip.
 * The server caps a page at 200 events, so a long run is a handful of
 * requests.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import type { RunReplay, RunReplayEvent } from "@/data/schemas";

const PAGE_SIZE = 200;

export const taskReplayKeys = {
  all: (wsId: string | null) => ["task-replay", wsId] as const,
  detail: (wsId: string | null, taskId: string) =>
    [...taskReplayKeys.all(wsId), taskId] as const,
};

export async function fetchFullReplay(
  taskId: string,
  signal?: AbortSignal,
): Promise<RunReplay | null> {
  const first = await api.getTaskReplay(taskId, { limit: PAGE_SIZE }, { signal });
  if (!first) return null;
  const events: RunReplayEvent[] = [...(first.events ?? [])];
  let cursor = first.next_cursor;
  // ponytail: sequential page walk; a run with >2000 events is not a phone
  // use case yet. Parallelise if replays grow.
  while (cursor !== null && cursor < first.total) {
    const page = await api.getTaskReplay(
      taskId,
      { cursor, limit: PAGE_SIZE },
      { signal },
    );
    if (!page) break;
    events.push(...(page.events ?? []));
    if (page.next_cursor === null || page.next_cursor <= cursor) break;
    cursor = page.next_cursor;
  }
  return { ...first, events, next_cursor: null };
}

export const taskReplayOptions = (wsId: string | null, taskId: string) =>
  queryOptions({
    queryKey: taskReplayKeys.detail(wsId, taskId),
    queryFn: ({ signal }) => fetchFullReplay(taskId, signal),
    enabled: !!wsId && !!taskId,
    // The chain only grows while the run is alive; refresh on focus is enough.
    staleTime: 30_000,
  });
