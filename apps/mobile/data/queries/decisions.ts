import { infiniteQueryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

/**
 * Durable decisions cache keys. Shape mirrors web
 * `packages/core/inbox/decisions.ts` (`["inboxDecisions", wsId, history]`).
 */
export const decisionKeys = {
  all: (wsId: string | null) => ["inboxDecisions", wsId] as const,
  list: (wsId: string | null, history: boolean) =>
    [...decisionKeys.all(wsId), history] as const,
};

export function inboxDecisionsOptions(
  wsId: string | null,
  history = false,
) {
  return infiniteQueryOptions({
    queryKey: decisionKeys.list(wsId, history),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam, signal }) => {
      const page = await api.listIssueDecisions(history, pageParam, {
        signal,
      });
      if (!page) throw new Error("Decisions unavailable");
      return page;
    },
    getNextPageParam: (page) => page.nextBeforeId ?? undefined,
    enabled: !!wsId,
    refetchInterval: 15_000,
  });
}
