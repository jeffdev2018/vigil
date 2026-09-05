import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

// Task watchdog (K73): an optional agent, different from the assignee, that
// inspects the issue subtree once it is at rest and returns a verdict.

export interface Watchdog {
  id: string;
  issue_id: string;
  agent_id: string;
  agent_name: string;
  owner_id: string;
  instructions: string;
  rest_minutes: number;
  enabled: boolean;
  last_scan_task_id: string | null;
  last_scanned_at: string | null;
  motion_streak: number;
  created_at: string;
}

export interface WatchdogInput {
  agent_id: string;
  owner_id?: string;
  instructions?: string;
  rest_minutes?: number;
  enabled?: boolean;
}

export interface WatchdogFinding {
  issue: string;
  issue_id: string;
  action: string;
  reason: string;
  missing_criterion: string;
}

export type WatchdogVerdictKind = "legitimate" | "motion" | "escalate";
export type WatchdogReview = "pending" | "confirmed" | "overturned";

export interface WatchdogVerdict {
  id: string;
  watchdog_id: string;
  issue_id: string;
  task_id: string;
  verdict: WatchdogVerdictKind;
  summary: string;
  findings: WatchdogFinding[];
  dropped: WatchdogFinding[];
  applied: Record<string, unknown>;
  decision_id: string | null;
  human_review: WatchdogReview;
  contract_revision: number;
  created_at: string;
}

export const watchdogKeys = {
  config: (wsId: string, issueId: string) => ["watchdog", wsId, issueId] as const,
  verdicts: (wsId: string, issueId: string) => ["watchdog", wsId, issueId, "verdicts"] as const,
};

export function issueWatchdogOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: watchdogKeys.config(wsId, issueId), queryFn: () => api.getIssueWatchdog(issueId) });
}

export function issueWatchdogVerdictsOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: watchdogKeys.verdicts(wsId, issueId), queryFn: () => api.listIssueWatchdogVerdicts(issueId) });
}

export function useSetIssueWatchdog(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: WatchdogInput) => api.setIssueWatchdog(issueId, body),
    onSettled: () => qc.invalidateQueries({ queryKey: watchdogKeys.config(wsId, issueId) }),
  });
}

export function useDeleteIssueWatchdog(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteIssueWatchdog(issueId),
    onSettled: () => qc.invalidateQueries({ queryKey: watchdogKeys.config(wsId, issueId) }),
  });
}

export function useScanIssueWatchdogNow(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.scanIssueWatchdogNow(issueId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: watchdogKeys.config(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useReviewWatchdogVerdict(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { verdictId: string; confirmed: boolean }) => api.reviewWatchdogVerdict(v.verdictId, v.confirmed),
    onSettled: () => qc.invalidateQueries({ queryKey: watchdogKeys.verdicts(wsId, issueId) }),
  });
}

/** What the verdict did, for one line: reopened / asked proofs / escalated / waiting for a decision / dismissed. */
export function watchdogOutcome(v: WatchdogVerdict): "reopened" | "asked_proof" | "escalated" | "awaiting_decision" | "dismissed" | "noted" | "pending" {
  const a = v.applied;
  if (a["escalated"] === true) return "escalated";
  if (a["dismissed"] === true) return "dismissed";
  if (typeof a["reopened"] === "number" && (a["reopened"] as number) > 0) return "reopened";
  if (typeof a["asked_proof"] === "number" && (a["asked_proof"] as number) > 0) return "asked_proof";
  if (a["noted"] === true) return "noted";
  if (v.decision_id && v.human_review === "pending") return "awaiting_decision";
  return "pending";
}
