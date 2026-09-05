import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Issue router (K27): the decision behind an issue's latest run and the
// workspace policy (pool per risk level, escalation threshold).

export type RiskLevel = "low" | "normal" | "high";
export const RISK_LEVELS: RiskLevel[] = ["low", "normal", "high"];

export interface RoutingDecision {
  risk_level: RiskLevel;
  matched_paths: string[];
  target_pool_id?: string;
  target_pool_name?: string;
  runtime_id?: string;
  escalated: boolean;
  escalation_reason?: string;
  decided_at: string;
}

export interface IssueRouting {
  decision: RoutingDecision | null;
  task_id: string | null;
  task_status?: string;
}

export interface RoutingSettings {
  enabled: boolean;
  pools: Record<string, string>;
  escalation_failures: number;
}

export const routingKeys = {
  issue: (wsId: string, issueId: string) => ["routing-decision", wsId, issueId] as const,
  settings: (wsId: string) => ["routing-settings", wsId] as const,
};

export function issueRoutingOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: routingKeys.issue(wsId, issueId), queryFn: () => api.getIssueRouting(issueId), staleTime: 30_000 });
}

export function routingSettingsOptions(wsId: string) {
  return queryOptions({ queryKey: routingKeys.settings(wsId), queryFn: () => api.getRoutingSettings() });
}

export function useSaveRoutingSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: RoutingSettings) => api.putRoutingSettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: routingKeys.settings(wsId) }),
  });
}
