import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";
import { fanoutKeys } from "./fanout";

// Refactoring campaigns (K42): a sharded fan-out whose branches are merged
// one at a time by a queue once F10 says they are ready.

export type CampaignMergeStatus = "pending" | "rebasing" | "ready" | "merged" | "conflict" | "skipped";

export interface CampaignBlocker {
  kind: string;
  label: string;
  count?: number;
  pr_number?: number;
}

export interface CampaignShard {
  id: string;
  child_issue_id: string;
  task_id: string;
  task_status: string;
  run_outcome: "completed" | "failed" | null;
  assignee_agent_id: string;
  description: string;
  branch_name: string;
  merge_position: number;
  merge_status: CampaignMergeStatus;
  merge_task_id: string | null;
  blockers: CampaignBlocker[];
  updated_at: string;
}

export interface RefactorCampaign {
  id: string;
  issue_id: string;
  fanout_batch_id: string;
  name: string;
  target_branch: string;
  status: "running" | "merging" | "completed" | "failed";
  shards: CampaignShard[];
  created_at: string;
  completed_at: string | null;
}

export interface CampaignInput {
  issue_id: string;
  name: string;
  target_branch: string;
  leader_agent_id: string;
  shards: { description: string; assignee_id: string; branch_name?: string }[];
}

export const campaignKeys = {
  issue: (wsId: string, issueId: string) => ["campaign", wsId, issueId] as const,
};

export function issueCampaignOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: campaignKeys.issue(wsId, issueId), queryFn: () => api.getIssueCampaign(issueId), refetchInterval: 10_000 });
}

export function useCreateCampaign(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Omit<CampaignInput, "issue_id">) => api.createCampaign({ ...input, issue_id: issueId }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: campaignKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: fanoutKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    },
  });
}

export function useSkipCampaignShard(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (shardId: string) => api.skipCampaignShard(shardId),
    onSettled: () => qc.invalidateQueries({ queryKey: campaignKeys.issue(wsId, issueId) }),
  });
}

/** Merged or skipped shards over all shards, 0..1. */
export function campaignProgress(c: RefactorCampaign): number {
  if (c.shards.length === 0) return 0;
  return c.shards.filter((s) => s.merge_status === "merged" || s.merge_status === "skipped").length / c.shards.length;
}

/** A shard a human can take out of the queue. */
export function campaignShardSkippable(s: CampaignShard): boolean {
  return s.merge_status === "pending" || s.merge_status === "ready" || s.merge_status === "conflict";
}
