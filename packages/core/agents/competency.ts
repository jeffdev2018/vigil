import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { OwnershipSuggestion } from "../types";

// Learned competency (K43): success history per agent and domain, shown
// next to the K27/K33 signals. Read-only for the client.

export interface CompetencyRow {
  agent_id: string;
  agent_name: string;
  domain_key: string;
  success_count: number;
  total_count: number;
  duel_wins: number;
  duel_losses: number;
  sample_size: number;
  score: number;
  reliable: boolean;
  updated_at: string;
}

export interface AgentCompetency {
  agent_id: string;
  min_sample: number;
  rows: CompetencyRow[];
}

export interface AssigneeSuggestion {
  domain_key: string;
  min_sample: number;
  candidates: CompetencyRow[];
  ownership: OwnershipSuggestion | null;
}

export interface CompetencySettings {
  min_sample: number;
}

// What-if estimate (K44): what a candidate has historically cost and
// taken on this issue's domain. Every number is null below min_sample —
// the server withholds rather than guesses, and so does the UI.
export interface IssueEstimateCandidate {
  agent_id: string;
  agent_name: string;
  sample_size: number;
  insufficient_history: boolean;
  median_cost_usd_ticks: number | null;
  cost_range_low_usd_ticks: number | null;
  cost_range_high_usd_ticks: number | null;
  median_duration_seconds: number | null;
  duration_range_low_seconds: number | null;
  duration_range_high_seconds: number | null;
  exceeds_budget: boolean;
}

export interface IssueEstimate {
  domain_key: string;
  min_sample: number;
  candidates: IssueEstimateCandidate[];
}

export const competencyKeys = {
  settings: (wsId: string) => ["competency", wsId, "settings"] as const,
  agent: (wsId: string, agentId: string) => ["competency", wsId, "agent", agentId] as const,
  issue: (wsId: string, issueId: string) => ["competency", wsId, "issue", issueId] as const,
  estimate: (wsId: string, issueId: string, candidates: string) => ["competency", wsId, "issue", issueId, "estimate", candidates] as const,
};

export function agentCompetencyOptions(wsId: string, agentId: string) {
  return queryOptions({ queryKey: competencyKeys.agent(wsId, agentId), queryFn: () => api.getAgentCompetency(agentId) });
}

export function assigneeSuggestionOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: competencyKeys.issue(wsId, issueId), queryFn: () => api.getAssigneeSuggestion(issueId) });
}

/** Sorted so the same set of candidates is one cache entry whatever the order. */
export function issueEstimateOptions(wsId: string, issueId: string, candidateIds: string[]) {
  const candidates = [...candidateIds].sort().join(",");
  return queryOptions({ queryKey: competencyKeys.estimate(wsId, issueId, candidates), queryFn: () => api.getIssueEstimate(issueId, candidateIds) });
}

const TICKS_PER_USD = 1e10;

function estimateUsd(ticks: number, decimals: number): string {
  return (Math.max(0, ticks) / TICKS_PER_USD).toFixed(decimals);
}

/** "$0.30–0.50" from a tick range; four decimals while the range is under a cent. */
export function estimateCostRange(lowTicks: number | null, highTicks: number | null): string {
  if (lowTicks === null || highTicks === null) return "";
  const [low, high] = lowTicks <= highTicks ? [lowTicks, highTicks] : [highTicks, lowTicks];
  const usdHigh = Math.max(0, high) / TICKS_PER_USD;
  const decimals = usdHigh > 0 && usdHigh < 0.01 ? 4 : 2;
  const from = estimateUsd(low, decimals);
  const to = estimateUsd(high, decimals);
  return from === to ? `$${from}` : `$${from}\u2013${to}`;
}

/** "8–15 min" from a second range; seconds under a minute, hours past one. */
export function estimateDurationRange(lowSeconds: number | null, highSeconds: number | null): string {
  if (lowSeconds === null || highSeconds === null) return "";
  const low = Math.max(0, lowSeconds <= highSeconds ? lowSeconds : highSeconds);
  const high = Math.max(0, lowSeconds <= highSeconds ? highSeconds : lowSeconds);
  const [divisor, decimals, unit] = high < 60 ? [1, 0, "s"] : high < 3600 ? [60, 0, " min"] : [3600, 1, " h"];
  const from = (low / divisor).toFixed(decimals);
  const to = (high / divisor).toFixed(decimals);
  return from === to ? `${from}${unit}` : `${from}\u2013${to}${unit}`;
}

/** "82%" from a 0..1 score. */
export function competencyRate(score: number): string {
  return `${Math.round(Math.max(0, Math.min(1, score)) * 100)}%`;
}

/** "label:backend" → "backend", "path:server" → "server/". */
export function competencyDomainLabel(domainKey: string): string {
  if (domainKey.startsWith("label:")) return domainKey.slice(6);
  if (domainKey.startsWith("path:")) return domainKey.slice(5) + "/";
  return domainKey;
}

export function competencySettingsOptions(wsId: string) {
  return queryOptions({ queryKey: competencyKeys.settings(wsId), queryFn: () => api.getCompetencySettings() });
}

export function useSaveCompetencySettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: CompetencySettings) => api.putCompetencySettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: ["competency", wsId] }),
  });
}
