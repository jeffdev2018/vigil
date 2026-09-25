import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

/** Three-segment key shape per apps/mobile/CLAUDE.md. `.all` is what a pack
 *  install invalidates: a bundle can bring paused agents with it. */
export const agentKeys = {
  all: (wsId: string | null) => ["agents", wsId] as const,
};

export const agentListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: agentKeys.all(wsId),
    queryFn: ({ signal }) => api.listAgents({ signal }),
    enabled: !!wsId,
    // Mirrors Web/Desktop: projected unstable ages offline without an event,
    // while offline recovery is covered by lifecycle events and reconnect.
    refetchInterval: (query) =>
      query.state.data?.some(
        (agent) =>
          !agent.archived_at &&
          (agent.runtime_availability === "online" ||
            agent.runtime_availability === "unstable"),
      )
        ? 30_000
        : false,
  });
