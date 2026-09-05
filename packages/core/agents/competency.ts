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

export const competencyKeys = {
  settings: (wsId: string) => ["competency", wsId, "settings"] as const,
  agent: (wsId: string, agentId: string) => ["competency", wsId, "agent", agentId] as const,
  issue: (wsId: string, issueId: string) => ["competency", wsId, "issue", issueId] as const,
};

export function agentCompetencyOptions(wsId: string, agentId: string) {
  return queryOptions({ queryKey: competencyKeys.agent(wsId, agentId), queryFn: () => api.getAgentCompetency(agentId) });
}

export function assigneeSuggestionOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: competencyKeys.issue(wsId, issueId), queryFn: () => api.getAssigneeSuggestion(issueId) });
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
