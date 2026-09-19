/**
 * Goal loop controls: pause / resume the chain, answer the agent's pending
 * question. Non-optimistic (the server owns the state machine — status,
 * continuation count and the question are all judge/server-decided), so
 * every mutation writes its response straight into the detail cache instead
 * of guessing a patch, same shape as `usePostmortemResolve` in
 * data/mutations/postmortem.ts.
 *
 * `useAnswerIssueGoal` can 409 ("nothing is waiting for an answer" — e.g.
 * two people answer at once, or the run moved on) — the caller branches on
 * `ApiError.status` to show a specific message instead of a generic one.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import type { IssueGoalResponse } from "@/data/schemas";
import { issueGoalKeys } from "@/data/queries/issue-goal";
import { useWorkspaceStore } from "@/data/workspace-store";

function useIssueGoalAction(
  issueId: string,
  action: () => Promise<IssueGoalResponse>,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation<IssueGoalResponse, Error, void>({
    mutationFn: action,
    onSuccess: (res) => {
      if (!wsId) return;
      qc.setQueryData(issueGoalKeys.issue(wsId, issueId), res);
    },
  });
}

export function useSetIssueGoal(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation<
    IssueGoalResponse,
    Error,
    { goal: string; max_continuations?: number }
  >({
    mutationFn: (body) => api.setIssueGoal(issueId, body),
    onSuccess: (res) => {
      if (!wsId) return;
      qc.setQueryData(issueGoalKeys.issue(wsId, issueId), res);
    },
  });
}

export function usePauseIssueGoal(issueId: string) {
  return useIssueGoalAction(issueId, () => api.pauseIssueGoal(issueId));
}

export function useResumeIssueGoal(issueId: string) {
  return useIssueGoalAction(issueId, () => api.resumeIssueGoal(issueId));
}

export function useAnswerIssueGoal(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation<IssueGoalResponse, Error, string>({
    mutationFn: (answer) => api.answerIssueGoal(issueId, answer),
    onSuccess: (res) => {
      if (!wsId) return;
      qc.setQueryData(issueGoalKeys.issue(wsId, issueId), res);
    },
  });
}
