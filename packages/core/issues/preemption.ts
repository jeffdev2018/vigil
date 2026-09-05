import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Preemption (K41): which runs of an issue were suspended to let an urgent
// issue go first, and whether they resumed.

export interface Preemption {
  task_id: string;
  status: string;
  preempted_at: string;
  preempted_by_task_id: string;
  preempted_by_issue_id: string | null;
  preempted_by_identifier: string | null;
  resumed_by_task_id: string | null;
}

export const preemptionKeys = {
  issue: (wsId: string, issueId: string) => ["preemptions", wsId, issueId] as const,
};

export function issuePreemptionsOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: preemptionKeys.issue(wsId, issueId), queryFn: () => api.listIssuePreemptions(issueId), refetchInterval: 15_000 });
}
