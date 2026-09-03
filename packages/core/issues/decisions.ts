import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { DecisionAnswer, IssueDecision } from "../types";
import { issueKeys } from "./queries";

export function issueDecisionsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueKeys.decisions(wsId, issueId),
    queryFn: ({ signal }) => api.listIssueDecisions(issueId, { signal }),
  });
}

export function useRespondIssueDecision(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { issueId: string; decisionId: string; answer: DecisionAnswer }) =>
      api.respondIssueDecision(v.issueId, v.decisionId, v.answer),
    onSettled: (_data, _err, v) => {
      qc.invalidateQueries({ queryKey: issueKeys.decisions(wsId, v.issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(v.issueId) });
    },
  });
}

export function isDecisionPending(d: Pick<IssueDecision, "response">): boolean {
  return d.response === null || d.response === undefined;
}

export function pendingDecisions(list: IssueDecision[]): IssueDecision[] {
  return list.filter(isDecisionPending);
}

/** Human-readable answer: the chosen option's label, or the modified text. */
export function decisionAnswerLabel(d: IssueDecision): string | null {
  if (!d.response) return null;
  if (d.response.modified_text) return d.response.modified_text;
  const option = d.options.find((o) => o.id === d.response?.option_id);
  return option?.label ?? d.response.option_id ?? null;
}
