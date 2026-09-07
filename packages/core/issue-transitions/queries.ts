import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const issueTransitionKeys = {
  rules: (wsId: string) => ["issue-transition-rules", wsId] as const,
  effective: (wsId: string, issueId: string) =>
    ["issue", issueId, wsId, "transition-effective"] as const,
  requests: (wsId: string, issueId: string) =>
    ["issue", issueId, wsId, "transition-requests"] as const,
};

export function issueTransitionRulesOptions(wsId: string) {
  return queryOptions({
    queryKey: issueTransitionKeys.rules(wsId),
    queryFn: () => api.listIssueTransitionRules(),
    enabled: !!wsId,
  });
}

export function effectiveIssueTransitionsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueTransitionKeys.effective(wsId, issueId),
    queryFn: () => api.getEffectiveIssueTransitions(issueId),
    enabled: !!wsId && !!issueId,
  });
}

export function issueTransitionRequestsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueTransitionKeys.requests(wsId, issueId),
    queryFn: () => api.listIssueTransitionRequests(issueId),
    enabled: !!wsId && !!issueId,
  });
}
