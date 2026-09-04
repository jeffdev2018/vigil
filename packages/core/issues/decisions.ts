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

/** Decision SLA (K35): what the card's deadline means right now. */
export type DecisionSlaState =
  | { kind: "none" }
  | { kind: "due"; deadline: Date }
  | { kind: "overdue"; deadline: Date }
  | { kind: "escalated_substitute"; escalatedAt: string | null }
  | { kind: "escalated_leads"; escalatedAt: string | null };

export function decisionSlaState(
  d: Pick<IssueDecision, "response" | "sla_deadline_at" | "escalation_level" | "escalated_at">,
  now: Date = new Date(),
): DecisionSlaState {
  if (!isDecisionPending(d)) return { kind: "none" };
  const level = d.escalation_level ?? 0;
  if (level >= 2) return { kind: "escalated_leads", escalatedAt: d.escalated_at ?? null };
  if (level === 1) return { kind: "escalated_substitute", escalatedAt: d.escalated_at ?? null };
  if (!d.sla_deadline_at) return { kind: "none" };
  const deadline = new Date(d.sla_deadline_at);
  if (Number.isNaN(deadline.getTime())) return { kind: "none" };
  return deadline.getTime() > now.getTime() ? { kind: "due", deadline } : { kind: "overdue", deadline };
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
