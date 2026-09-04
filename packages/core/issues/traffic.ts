import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Traffic control (K18): a run editing paths a human or another run is
// editing. Alert first; the human decides (pause, ignore).

export interface TrafficConflict {
  id: string;
  task_id: string;
  kind: "human" | "agent";
  paths: string[];
  other_task_id: string | null;
  handoff_packet_id: string | null;
  status: "active" | "ignored" | "resolved";
  created_at: string;
  resolved_at: string | null;
}

export const trafficKeys = {
  conflicts: (wsId: string, issueId: string) => ["traffic-conflicts", wsId, issueId] as const,
};

export function issueTrafficConflictsOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: trafficKeys.conflicts(wsId, issueId), queryFn: () => api.listTrafficConflicts(issueId), refetchInterval: 15_000 });
}

export function useIgnoreTrafficConflict(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (conflictId: string) => api.ignoreTrafficConflict(issueId, conflictId),
    onSettled: () => qc.invalidateQueries({ queryKey: trafficKeys.conflicts(wsId, issueId) }),
  });
}
