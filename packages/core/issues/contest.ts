import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Contest (K72): a rival model challenges an agent output with numbered
// objections, the author answers each, a human gives the verdict.

export type ContestTargetType = "task_result" | "plan" | "triage_verdict" | "meeting_summary";
export type ContestStatus = "running" | "objections_ready" | "answering" | "answered" | "confirmed" | "failed";
export type ContestVerdict = "upheld" | "dismissed" | "mixed";

export interface ContestObjection {
  n: number;
  severity: "high" | "medium" | "low";
  kind: "missing" | "false" | "risky";
  claim: string;
  evidence: string;
  expected_proof: string;
}

export interface ContestAnswer {
  n: number;
  verdict: "accept" | "refute" | "fix";
  note: string;
  proof: string;
}

export interface Contest {
  id: string;
  workspace_id: string;
  project_id: string | null;
  issue_id: string | null;
  target_type: ContestTargetType;
  target_id: string;
  target_excerpt: string;
  author_agent_id: string | null;
  author_provider: string;
  challenger_kind: "agent" | "llm";
  challenger_agent_id: string | null;
  challenger_provider: string;
  same_vendor: boolean;
  challenger_task_id: string | null;
  answer_task_id: string | null;
  round: number;
  max_rounds: number;
  objections: ContestObjection[];
  answers: ContestAnswer[];
  nothing_to_contest: string;
  status: ContestStatus;
  human_verdict: ContestVerdict | null;
  verdict_note: string;
  confirmed_by: string | null;
  confirmed_at: string | null;
  auto: boolean;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface ContestPreflight {
  target_type: string;
  target_id: string;
  issue_id: string | null;
  author_agent_id: string | null;
  author_provider: string;
  challenger: { kind: "agent" | "llm"; agent_id: string; name: string; provider: string; same_vendor: boolean };
  estimated_cost_usd_ticks: number;
  quota_used: number;
  quota_limit: number;
  max_rounds: number;
  existing: number;
}

export interface ContestSettings {
  targets: Record<string, boolean>;
  opt_out_project_ids: string[];
}

export const CONTEST_TARGET_TYPES: ContestTargetType[] = ["task_result", "plan", "triage_verdict", "meeting_summary"];

export const contestKeys = {
  all: (wsId: string) => ["contests", wsId] as const,
  issue: (wsId: string, issueId: string) => ["contests", wsId, "issue", issueId] as const,
  target: (wsId: string, targetType: string, targetId: string) => ["contests", wsId, "target", targetType, targetId] as const,
  preflight: (wsId: string, targetType: string, targetId: string) => ["contests", wsId, "preflight", targetType, targetId] as const,
  settings: (wsId: string) => ["contests", wsId, "settings"] as const,
};

/** Live while a run is on either side; settled otherwise. */
export function contestIsLive(c: Pick<Contest, "status">): boolean {
  return c.status === "running" || c.status === "answering";
}

export function issueContestsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: contestKeys.issue(wsId, issueId),
    queryFn: () => api.listContests({ issue_id: issueId }),
    refetchInterval: (q) => ((q.state.data ?? []).some(contestIsLive) ? 10_000 : false),
  });
}

export function targetContestsOptions(wsId: string, targetType: ContestTargetType, targetId: string) {
  return queryOptions({
    queryKey: contestKeys.target(wsId, targetType, targetId),
    queryFn: () => api.listContests({ target_type: targetType, target_id: targetId }),
    refetchInterval: (q) => ((q.state.data ?? []).some(contestIsLive) ? 10_000 : false),
  });
}

export function contestPreflightOptions(wsId: string, targetType: ContestTargetType, targetId: string, enabled = true) {
  return queryOptions({
    queryKey: contestKeys.preflight(wsId, targetType, targetId),
    queryFn: () => api.preflightContest({ target_type: targetType, target_id: targetId }),
    enabled,
    staleTime: 0,
  });
}

export function contestSettingsOptions(wsId: string) {
  return queryOptions({ queryKey: contestKeys.settings(wsId), queryFn: () => api.getContestSettings() });
}

export function useCreateContest(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { target_type: ContestTargetType; target_id: string; max_rounds?: number; challenger_agent_id?: string }) => api.createContest(v),
    onSettled: () => qc.invalidateQueries({ queryKey: contestKeys.all(wsId) }),
  });
}

export function useConfirmContest(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; verdict: ContestVerdict; note: string }) => api.confirmContest(v.id, v.verdict, v.note),
    onSettled: () => qc.invalidateQueries({ queryKey: contestKeys.all(wsId) }),
  });
}

export function useSaveContestSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: ContestSettings) => api.putContestSettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: contestKeys.settings(wsId) }),
  });
}

/** Objections paired with the author's answer, in objection order. */
export function pairContestRows(c: Pick<Contest, "objections" | "answers">): { objection: ContestObjection; answer: ContestAnswer | null }[] {
  const byN = new Map(c.answers.map((a) => [a.n, a] as const));
  return c.objections.map((o) => ({ objection: o, answer: byN.get(o.n) ?? null }));
}

/** USD from cost ticks (1 tick = 1e-6 USD), two decimals. */
export function contestCostUsd(ticks: number): string {
  return (ticks / 1_000_000).toFixed(2);
}
