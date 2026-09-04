import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";

// Trust Dial (K26): the agent's autonomy mode, the promotion its scorecard
// earns, and the log of every change. A mode changes only through the PUT.

export const TRUST_MODES = ["observer", "propose", "approval", "autonomous"] as const;
export type TrustMode = (typeof TRUST_MODES)[number];

export interface TrustMetrics {
  days: number;
  runs_total: number;
  accepted_rate: number;
  no_intervention_rate: number;
  reopen_rate: number;
}

export interface TrustSuggestion {
  eligible: boolean;
  current_mode: string;
  suggested_mode?: string;
  metrics: TrustMetrics;
  thresholds: { days: number; min_runs: number; min_accepted_rate: number; min_no_intervention_rate: number; max_reopen_rate: number };
  reasons: string[];
}

export interface TrustModeChange {
  id: string;
  from_mode: string;
  to_mode: string;
  reason: string | null;
  triggered_by_type: string;
  triggered_by_id: string | null;
  created_at: string;
  demotion: boolean;
}

export const trustKeys = {
  mode: (wsId: string, agentId: string) => ["agents", wsId, agentId, "trust"] as const,
  suggestion: (wsId: string, agentId: string) => ["agents", wsId, agentId, "trust", "suggestion"] as const,
  history: (wsId: string, agentId: string) => ["agents", wsId, agentId, "trust", "history"] as const,
};

export function agentTrustModeOptions(wsId: string, agentId: string) {
  return queryOptions({ queryKey: trustKeys.mode(wsId, agentId), queryFn: () => api.getAgentTrustMode(agentId) });
}

export function agentTrustSuggestionOptions(wsId: string, agentId: string) {
  return queryOptions({ queryKey: trustKeys.suggestion(wsId, agentId), queryFn: () => api.getAgentTrustSuggestion(agentId), staleTime: 60_000 });
}

export function agentTrustHistoryOptions(wsId: string, agentId: string) {
  return queryOptions({ queryKey: trustKeys.history(wsId, agentId), queryFn: () => api.listAgentTrustHistory(agentId) });
}

export function useSetAgentTrustMode(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { mode: TrustMode; reason?: string }) => api.setAgentTrustMode(agentId, v.mode, v.reason),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: trustKeys.mode(wsId, agentId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agent(wsId, agentId) });
    },
  });
}

/** Percent for display, from a 0..1 rate. */
export function pct(rate: number): string {
  return `${Math.round(rate * 100)}%`;
}
