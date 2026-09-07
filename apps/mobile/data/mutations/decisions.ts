/**
 * Decision mutations. Await the server (no optimistic answer) — a final
 * response is durable and concurrent writers must surface 409, matching
 * packages/core/inbox/decisions.ts.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import { decisionKeys } from "@/data/queries/decisions";
import { useWorkspaceStore } from "@/data/workspace-store";

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

export function useAnswerIssueDecision(
  issueId: string,
  decisionId: string,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: async ({
      status,
      answer,
    }: {
      status: "answered" | "cancelled";
      answer: string;
    }) => {
      const result = await api.answerIssueDecision(
        issueId,
        decisionId,
        status,
        answer,
      );
      if (!result) throw new Error("Answer receipt unavailable");
      return result;
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: decisionKeys.all(wsId) });
    },
  });
}

export function useResumeIssueDecision(
  issueId: string,
  decisionId: string,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: async () => {
      const result = await api.resumeIssueDecision(issueId, decisionId);
      if (!result) throw new Error("Resume receipt unavailable");
      return result;
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: decisionKeys.all(wsId) });
    },
  });
}

export function decisionAnswerErrorMessage(err: unknown): string {
  if (httpStatus(err) === 409) {
    return "This request already has a final response. Reload to see it.";
  }
  return "Your answer could not be confirmed. Try again with the same answer.";
}

export function decisionResumeErrorMessage(err: unknown): string {
  const status = httpStatus(err);
  if (status === 409) {
    return "Your answer is saved. Finish the source run, wait for pending work, or restore the agent runtime before trying again.";
  }
  if (status === 403) {
    return "Your answer is saved. You do not have permission to run this agent.";
  }
  return "Your answer is saved. The follow-up could not be confirmed. Retry to recover the same run receipt.";
}
