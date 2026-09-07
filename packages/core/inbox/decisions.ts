import { infiniteQueryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
export type { IssueDecision } from "../api/schemas";

export function inboxDecisionsOptions(wsId: string, history = false) {
  return infiniteQueryOptions({
    queryKey: ["inboxDecisions", wsId, history],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const page = await api.listIssueDecisions(history, pageParam);
      if (!page) throw new Error("Decisions unavailable");
      return page;
    },
    getNextPageParam: (page) => page.nextBeforeId ?? undefined,
    enabled: Boolean(wsId),
    refetchInterval: 15_000,
  });
}

export function useAnswerIssueDecision(wsId: string, issueId: string, decisionId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ status, answer }: { status: "answered" | "cancelled"; answer: string }) => {
      const result = await api.answerIssueDecision(issueId, decisionId, status, answer);
      if (!result) throw new Error("Answer receipt unavailable");
      return result;
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["inboxDecisions", wsId] }),
  });
}

export function useResumeIssueDecision(wsId: string, issueId: string, decisionId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const result = await api.resumeIssueDecision(issueId, decisionId);
      if (!result) throw new Error("Resume receipt unavailable");
      return result;
    },
    onSettled: () => Promise.all([
      qc.invalidateQueries({ queryKey: ["inboxDecisions", wsId] }),
      qc.invalidateQueries({ queryKey: ["issues", "tasks", wsId, issueId] }),
    ]),
  });
}
