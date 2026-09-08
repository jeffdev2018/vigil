import { infiniteQueryOptions, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

export type { IssueDelivery, DeliveryReview } from "../api/schemas";

export interface ReviewDeliveryInput {
  reviewId: string;
  expectedReviewId: string;
  snapshotToken: string;
  decision: "accepted" | "changes_requested";
  feedback: string;
  assessments: { passed: boolean; evidence: string }[];
  /** Client-timed active review seconds. Distinct from wall-clock reviewDelaySeconds. */
  humanEffortSeconds?: number | null;
}

export const deliveryKeys = {
  all: (wsId: string) => ["issueDelivery", wsId] as const,
  detail: (wsId: string, issueId: string) => ["issueDelivery", wsId, issueId] as const,
};

export function issueDeliveryOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: deliveryKeys.detail(wsId, issueId),
    queryFn: async () => {
      const delivery = await api.getIssueDelivery(issueId);
      if (!delivery) throw new Error("Delivery evidence unavailable");
      return delivery;
    },
    enabled: Boolean(wsId && issueId),
    // Reconcile missed events and time-based CI staleness while the card is visible.
    refetchInterval: 30_000,
  });
}

export function deliveryTasksOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: ["issues", "tasks", wsId, issueId],
    queryFn: async () => (await api.listTasksByIssue(issueId)).filter((task) => !task.chat_session_id),
    enabled: Boolean(wsId && issueId),
    staleTime: 30_000,
  });
}

export function useUpdateDeliveryCriteria(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ criteria, expectedRevision }: { criteria: string[]; expectedRevision: number }) => {
      const saved = await api.updateIssueDeliveryCriteria(issueId, criteria, expectedRevision);
      if (!saved) throw new Error("Criteria response unavailable");
      return saved;
    },
    onSettled: () => qc.invalidateQueries({ queryKey: issueDeliveryOptions(wsId, issueId).queryKey }),
  });
}

export function useReviewDelivery(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: ReviewDeliveryInput) => {
      const saved = await api.reviewIssueDelivery(issueId, input);
      if (!saved) throw new Error("Review response unavailable");
      return saved;
    },
    onSettled: () => qc.invalidateQueries({ queryKey: issueDeliveryOptions(wsId, issueId).queryKey }),
  });
}

export function useStartDeliveryCorrection(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (reviewId: string) => {
      const receipt = await api.startIssueDeliveryCorrection(issueId, reviewId);
      if (!receipt) throw new Error("Correction response unavailable");
      return receipt;
    },
    onSettled: () => Promise.all([
      qc.invalidateQueries({ queryKey: deliveryKeys.detail(wsId, issueId) }),
      qc.invalidateQueries({ queryKey: deliveryTasksOptions(wsId, issueId).queryKey }),
    ]),
  });
}

export function deliveryHistoryOptions(wsId: string, issueId: string) {
  return infiniteQueryOptions({
    queryKey: [...deliveryKeys.detail(wsId, issueId), "history"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const history = await api.getIssueDeliveryHistory(issueId, pageParam);
      if (!history) throw new Error("Delivery history unavailable");
      return history;
    },
    getNextPageParam: (page) => page.nextBeforeId ?? undefined,
    enabled: Boolean(wsId && issueId),
  });
}
