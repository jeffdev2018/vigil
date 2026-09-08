/**
 * Delivery mutations. Await the server (no optimistic accept) — concurrent
 * writers must surface 409, matching packages/core/issues/delivery.ts.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import { deliveryKeys } from "@/data/queries/delivery";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWorkspaceStore } from "@/data/workspace-store";

/** Mirrors packages/core/issues/delivery.ts ReviewDeliveryInput. */
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

function httpStatus(err: unknown): number | undefined {
  if (
    err &&
    typeof err === "object" &&
    "status" in err &&
    typeof (err as { status: unknown }).status === "number"
  ) {
    return (err as { status: number }).status;
  }
  return undefined;
}

export function useUpdateDeliveryCriteria(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: async ({
      criteria,
      expectedRevision,
    }: {
      criteria: string[];
      expectedRevision: number;
    }) => {
      const saved = await api.updateIssueDeliveryCriteria(
        issueId,
        criteria,
        expectedRevision,
      );
      if (!saved) throw new Error("Criteria response unavailable");
      return saved;
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: deliveryKeys.detail(wsId, issueId) });
    },
  });
}

export function useReviewDelivery(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: async (input: ReviewDeliveryInput) => {
      const saved = await api.reviewIssueDelivery(issueId, input);
      if (!saved) throw new Error("Review response unavailable");
      return saved;
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: deliveryKeys.detail(wsId, issueId) });
    },
  });
}

export function useStartDeliveryCorrection(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: async (reviewId: string) => {
      const receipt = await api.startIssueDeliveryCorrection(issueId, reviewId);
      if (!receipt) throw new Error("Correction response unavailable");
      return receipt;
    },
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: deliveryKeys.detail(wsId, issueId) }),
        qc.invalidateQueries({ queryKey: issueKeys.tasks(wsId, issueId) }),
        qc.invalidateQueries({
          queryKey: issueKeys.activeTasks(wsId, issueId),
        }),
      ]),
  });
}

export function deliverySaveErrorMessage(err: unknown): string {
  if (httpStatus(err) === 409) {
    return "The delivery or review changed. Close this draft and reload before deciding. Your draft has been kept.";
  }
  return "Could not confirm the save. Your draft has been kept; retry or reload to check.";
}

export function deliveryCorrectionErrorMessage(err: unknown): string {
  if (httpStatus(err) === 409) {
    return "The correction could not be confirmed. Your review is saved. Reload to check the result; retrying this review will not create a second correction run.";
  }
  return "The correction could not be confirmed. Your review is saved. Reload to check the result; retrying this review will not create a second correction run.";
}
