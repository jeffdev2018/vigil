import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

/**
 * Delivery cache keys. Shape mirrors web
 * `packages/core/issues/delivery.ts` (`["issueDelivery", wsId, issueId]`).
 */
export const deliveryKeys = {
  all: (wsId: string | null) => ["issueDelivery", wsId] as const,
  detail: (wsId: string | null, issueId: string) =>
    [...deliveryKeys.all(wsId), issueId] as const,
};

export function issueDeliveryOptions(wsId: string | null, issueId: string) {
  return queryOptions({
    queryKey: deliveryKeys.detail(wsId, issueId),
    queryFn: async ({ signal }) => {
      const delivery = await api.getIssueDelivery(issueId, { signal });
      if (!delivery) throw new Error("Delivery evidence unavailable");
      return delivery;
    },
    enabled: Boolean(wsId && issueId),
    // Reconcile missed events and time-based CI staleness while the card is visible.
    refetchInterval: 30_000,
  });
}
