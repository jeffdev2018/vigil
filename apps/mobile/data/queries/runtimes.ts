import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

/** Three-segment key shape per apps/mobile/CLAUDE.md. */
export const runtimeKeys = {
  all: (wsId: string | null) => ["runtimes", wsId] as const,
};

// Runtime list — workspace-scoped. Feeds the availability dimension of the
// presence dot via @multica/core/agents/derive-presence (status + last_seen_at).
// Invalidated by daemon:register / sweeper-driven status changes; see
// data/realtime/use-presence-realtime.ts.
export const runtimeListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: runtimeKeys.all(wsId),
    queryFn: ({ signal }) => api.listRuntimes({ signal }),
    enabled: !!wsId,
  });
