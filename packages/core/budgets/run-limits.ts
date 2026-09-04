import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Run limits (K03): caps on one run (cost, duration, turns, tool calls) at
// workspace, project or agent scope. Period budgets live in ./queries.

export type RunLimitGate = "cost" | "duration" | "turns" | "tool_calls";
export const RUN_LIMIT_GATES: RunLimitGate[] = ["cost", "duration", "turns", "tool_calls"];

export interface RunLimitPolicy {
  id: string;
  scope_type: "workspace" | "project" | "agent";
  scope_id: string | null;
  max_cost_usd_ticks: number | null;
  max_duration_seconds: number | null;
  max_turns: number | null;
  max_tool_calls: number | null;
  warn_bps: number;
  action: "observe" | "enforce";
  created_at: string;
}

export type RunLimitPolicyInput = Omit<RunLimitPolicy, "id" | "created_at">;

export interface RunLimitEvent {
  task_id: string;
  gate: RunLimitGate;
  level: "warn" | "exceeded" | "stopped";
  observed: number;
  limit: number;
  policy_id: string;
  created_at: string;
}

export const runLimitKeys = {
  list: (wsId: string) => ["run-limits", wsId] as const,
  issueEvents: (wsId: string, issueId: string) => ["run-limit-events", wsId, issueId] as const,
};

export function runLimitPoliciesOptions(wsId: string) {
  return queryOptions({ queryKey: runLimitKeys.list(wsId), queryFn: () => api.listRunLimitPolicies() });
}

export function issueRunLimitEventsOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: runLimitKeys.issueEvents(wsId, issueId), queryFn: () => api.listIssueRunLimitEvents(issueId), staleTime: 15_000 });
}

export function useSaveRunLimitPolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id?: string; input: RunLimitPolicyInput }) => (v.id ? api.updateRunLimitPolicy(v.id, v.input) : api.createRunLimitPolicy(v.input)),
    onSettled: () => qc.invalidateQueries({ queryKey: runLimitKeys.list(wsId) }),
  });
}

export function useDeleteRunLimitPolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteRunLimitPolicy(id),
    onSettled: () => qc.invalidateQueries({ queryKey: runLimitKeys.list(wsId) }),
  });
}

/** Human value of an observed/limit number for a gate. */
export function formatGateValue(gate: RunLimitGate, value: number): string {
  switch (gate) {
    case "cost":
      return `$${(value / 1e10).toFixed(2)}`;
    case "duration":
      return value >= 3600 ? `${Math.floor(value / 3600)}h${Math.floor((value % 3600) / 60).toString().padStart(2, "0")}` : `${Math.floor(value / 60)}m${(value % 60).toString().padStart(2, "0")}s`;
    default:
      return String(value);
  }
}
