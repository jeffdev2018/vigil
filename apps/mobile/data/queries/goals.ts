/**
 * Workspace goal queries (K74). One shape for now: the list
 * (`goalKeys.list`) → `Goal[]`, read-only on mobile.
 *
 * Key shape mirrors `packages/core/goals/queries.ts` (`["goals", wsId, "list"]`)
 * so a reader switching clients finds the same cache layout. No realtime
 * hook: the server emits no `goal:*` WS events yet, so pull-to-refresh is
 * the only refresh path.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const goalKeys = {
  all: (wsId: string | null) => ["goals", wsId] as const,
  list: (wsId: string | null) => [...goalKeys.all(wsId), "list"] as const,
};

export const goalListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: goalKeys.list(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listGoals({ signal });
      return res.goals;
    },
    enabled: !!wsId,
  });
