import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { githubKeys } from "../github/queries";

// CI auto-fix (K49): the correction runs launched on an agent's red pull
// request, the workspace policy, and the manual retry past the cap.

export interface CIAutoFixRun {
  id: string;
  provider: string;
  pull_request_id: string;
  head_sha: string;
  issue_id: string;
  task_id: string | null;
  task_status: string;
  attempt: number;
  budget_usd_ticks: number;
  manual: boolean;
  created_at: string;
}

export interface IssueCIAutoFix {
  runs: CIAutoFixRun[];
  enabled: boolean;
  max_attempts: number;
}

export interface CIAutoFixSettings {
  enabled: boolean;
  max_attempts: number;
  budget_usd_ticks: number;
}

export const ciAutoFixKeys = {
  issue: (wsId: string, issueId: string) => ["ci-auto-fix", wsId, issueId] as const,
  settings: (wsId: string) => ["ci-auto-fix", wsId, "settings"] as const,
};

export function issueCIAutoFixOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: ciAutoFixKeys.issue(wsId, issueId), queryFn: () => api.getIssueCIAutoFix(issueId), refetchInterval: 15_000 });
}

export function useRetryCIAutoFix(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (pullRequestId: string) => api.retryCIAutoFix(pullRequestId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ciAutoFixKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: githubKeys.pullRequests(issueId) });
    },
  });
}

export function ciAutoFixSettingsOptions(wsId: string) {
  return queryOptions({ queryKey: ciAutoFixKeys.settings(wsId), queryFn: () => api.getCIAutoFixSettings() });
}

export function useSaveCIAutoFixSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: CIAutoFixSettings) => api.putCIAutoFixSettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: ciAutoFixKeys.settings(wsId) }),
  });
}

const activeStatuses = new Set(["queued", "dispatched", "running", "waiting_local_directory", "deferred", "paused"]);

export type CIAutoFixState = "none" | "in_progress" | "fixed" | "failed" | "exhausted";

/** The chip state of one pull request from its correction runs (newest first). */
export function ciAutoFixState(runs: CIAutoFixRun[], pullRequestId: string, maxAttempts: number): { state: CIAutoFixState; attempts: number } {
  const mine = runs.filter((r) => r.pull_request_id === pullRequestId);
  if (mine.length === 0) return { state: "none", attempts: 0 };
  const latest = mine[0]!;
  if (activeStatuses.has(latest.task_status)) return { state: "in_progress", attempts: mine.length };
  if (latest.task_status === "completed") return { state: "fixed", attempts: mine.length };
  if (mine.length >= maxAttempts) return { state: "exhausted", attempts: mine.length };
  return { state: "failed", attempts: mine.length };
}
