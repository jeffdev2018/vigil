import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";

// Runtime pools (K28): ordered families of interchangeable runtimes with an
// explicit degraded last resort. Failover moves are read per issue.

export interface RuntimePool {
  id: string;
  name: string;
  runtime_ids: string[];
  degraded_runtime_id: string | null;
  agent_count: number;
  created_at: string;
}

export interface RuntimePoolInput {
  name: string;
  runtime_ids: string[];
  degraded_runtime_id: string | null;
}

export interface FailoverMove {
  from_runtime_id: string;
  to_runtime_id: string;
  reason: string;
  degraded: boolean;
  at: string;
}

export interface RunFailover {
  task_id: string;
  status: string;
  degraded: boolean;
  failure_reason?: string;
  moves: FailoverMove[];
}

export const runtimePoolKeys = {
  list: (wsId: string) => ["runtime-pools", wsId] as const,
  failovers: (wsId: string, issueId: string) => ["issues", wsId, "failover-history", issueId] as const,
};

export function runtimePoolsOptions(wsId: string) {
  return queryOptions({ queryKey: runtimePoolKeys.list(wsId), queryFn: () => api.listRuntimePools(), staleTime: 30_000 });
}

export function issueFailoverHistoryOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: runtimePoolKeys.failovers(wsId, issueId), queryFn: () => api.listIssueFailoverHistory(issueId), staleTime: 30_000 });
}

export function useSaveRuntimePool(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id?: string; input: RuntimePoolInput }) => (v.id ? api.updateRuntimePool(v.id, v.input) : api.createRuntimePool(v.input)),
    onSettled: () => qc.invalidateQueries({ queryKey: runtimePoolKeys.list(wsId) }),
  });
}

export function useDeleteRuntimePool(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteRuntimePool(id),
    onSettled: () => qc.invalidateQueries({ queryKey: runtimePoolKeys.list(wsId) }),
  });
}

export function useSetAgentRuntimePool(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (poolId: string | null) => api.setAgentRuntimePool(agentId, poolId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agent(wsId, agentId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      qc.invalidateQueries({ queryKey: runtimePoolKeys.list(wsId) });
    },
  });
}

/** Move an id one step up or down in an ordered list; out of range is a no-op. */
export function moveInList(list: string[], id: string, delta: -1 | 1): string[] {
  const i = list.indexOf(id);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= list.length) return list;
  const out = [...list];
  [out[i], out[j]] = [out[j] as string, out[i] as string];
  return out;
}
