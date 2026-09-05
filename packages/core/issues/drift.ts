import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Drift detection (K40): the workspace thresholds behind "stopped for
// going in circles". The reason itself rides on the task (drift_reason).

export type DriftReason = "repeated_action" | "file_reread_loop";

export interface DriftPolicy {
  enabled: boolean;
  repeated_action_threshold: number;
  file_reread_threshold: number;
}

export const driftKeys = {
  policy: (wsId: string) => ["drift-policy", wsId] as const,
};

export function driftPolicyOptions(wsId: string) {
  return queryOptions({ queryKey: driftKeys.policy(wsId), queryFn: () => api.getDriftPolicy() });
}

export function useSaveDriftPolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: DriftPolicy) => api.putDriftPolicy(v),
    onSettled: () => qc.invalidateQueries({ queryKey: driftKeys.policy(wsId) }),
  });
}
