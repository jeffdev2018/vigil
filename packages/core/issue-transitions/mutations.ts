import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import { issueTransitionKeys } from "./queries";
import type { IssueTransitionRuleWrite } from "../api";

export function useSaveIssueTransitionRule(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...data }: IssueTransitionRuleWrite & { id?: string }) =>
      id ? api.updateIssueTransitionRule(id, data) : api.createIssueTransitionRule(data),
    onSettled: () => qc.invalidateQueries({ queryKey: issueTransitionKeys.rules(wsId) }),
  });
}

export function useDeleteIssueTransitionRule(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteIssueTransitionRule(id),
    onSettled: () => qc.invalidateQueries({ queryKey: issueTransitionKeys.rules(wsId) }),
  });
}

// Deciding a held transition can move the issue, so both the request list and
// the issue's own caches are refreshed. Never optimistic: the whole point of
// the gate is that the server, not this client, decides whether it applies.
export function useDecideIssueTransitionRequest(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ requestId, decision, note }: {
      requestId: string;
      decision: "approve" | "reject";
      note?: string;
    }) => api.decideIssueTransitionRequest(requestId, decision, note),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueTransitionKeys.requests(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueTransitionKeys.effective(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    },
  });
}

export function useCancelIssueTransitionRequest(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (requestId: string) => api.cancelIssueTransitionRequest(requestId),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: issueTransitionKeys.requests(wsId, issueId) }),
  });
}
