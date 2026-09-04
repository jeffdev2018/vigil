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

/** Requirement Interview (K13): the cards of one interview, in asked order. */
export interface DecisionInterview {
  groupId: string;
  decisions: IssueDecision[];
  answered: number;
}

/**
 * Splits a card list into interviews (cards sharing a group, ordered by
 * position) and single cards, keeping the list's order of first appearance.
 */
export function groupDecisions(list: IssueDecision[]): { interviews: DecisionInterview[]; singles: IssueDecision[] } {
  const interviews: DecisionInterview[] = [];
  const byGroup = new Map<string, DecisionInterview>();
  const singles: IssueDecision[] = [];
  for (const d of list) {
    if (!d.interview_group_id) {
      singles.push(d);
      continue;
    }
    let group = byGroup.get(d.interview_group_id);
    if (!group) {
      group = { groupId: d.interview_group_id, decisions: [], answered: 0 };
      byGroup.set(d.interview_group_id, group);
      interviews.push(group);
    }
    group.decisions.push(d);
    if (!isDecisionPending(d)) group.answered += 1;
  }
  for (const g of interviews) g.decisions.sort((a, b) => (a.interview_position ?? 0) - (b.interview_position ?? 0));
  return { interviews, singles };
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
