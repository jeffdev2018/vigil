import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

// Fan-out / fan-in (K38): parallel specialist runs on child issues and the
// leader's synthesis once the barrier settles.

export interface FanoutMember {
  id: string;
  child_issue_id: string;
  task_id: string;
  task_status: string;
  assignee_agent_id: string;
  description: string;
  outcome: "completed" | "failed" | null;
  settled_at: string | null;
}

export interface FanoutBatch {
  id: string;
  parent_issue_id: string;
  leader_agent_id: string;
  status: "pending" | "partial_failure" | "complete";
  expected_count: number;
  completed_count: number;
  failed_count: number;
  synthesis_task_id: string | null;
  members: FanoutMember[];
  created_at: string;
  completed_at: string | null;
}

export interface FanoutInput {
  leader_agent_id: string;
  sub_tasks: { description: string; assignee_id: string }[];
}

export const fanoutKeys = {
  issue: (wsId: string, issueId: string) => ["fanout", wsId, issueId] as const,
};

export function issueFanoutOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: fanoutKeys.issue(wsId, issueId), queryFn: () => api.getIssueFanout(issueId), refetchInterval: 10_000 });
}

export function useStartFanout(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: FanoutInput) => api.startFanout(issueId, input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: fanoutKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    },
  });
}

/** Settled members over expected, 0..1. */
export function fanoutProgress(b: FanoutBatch): number {
  return b.expected_count === 0 ? 0 : (b.completed_count + b.failed_count) / b.expected_count;
}
