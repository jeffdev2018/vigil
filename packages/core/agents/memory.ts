import { infiniteQueryOptions, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { AgentMemoryStatus, MemoryExecutionRequest } from "../types";

export const agentMemoryKeys = {
  all: (wsId: string) => ["agent-memories", wsId] as const,
  list: (wsId: string, agentId: string) =>
    [...agentMemoryKeys.all(wsId), agentId] as const,
};

// Memories of a single agent (agent detail page, Memory tab). WS
// agent_memory.* events invalidate this via useRealtimeSync.
export function agentMemoryOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: agentMemoryKeys.list(wsId, agentId),
    queryFn: () => api.listAgentMemories(agentId),
    enabled: Boolean(wsId && agentId),
    refetchInterval: (query) =>
      query.state.data?.memories.some((memory) => memory.expires_at && !memory.expired) ? 30_000 : false,
  });
}

export function useCreateAgentMemory(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ content, source_task_id, expires_at, source_review_id }: { content: string; source_task_id?: string; expires_at?: string | null; source_review_id?: string }) => api.createAgentMemory(agentId, content, source_task_id, expires_at, source_review_id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) });
    },
  });
}

export function useUpdateAgentMemory(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ memoryId, ...changes }: {
      memoryId: string;
      content?: string;
      status?: AgentMemoryStatus;
      expected_revision?: number;
      expires_at?: string | null;
      restore_revision?: number;
      evaluation_id?: string;
    }) => api.updateAgentMemory(agentId, memoryId, changes),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) });
    },
  });
}

// No optimistic removal: the row disappears only after the server confirms,
// matching the labels delete pattern.
export function useDeleteAgentMemory(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (memoryId: string) => api.deleteAgentMemory(agentId, memoryId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) });
    },
  });
}

export function agentMemoryHistoryOptions(wsId: string, agentId: string, memoryId: string) {
  return infiniteQueryOptions({
    queryKey: [...agentMemoryKeys.list(wsId, agentId), "history", memoryId],
    initialPageParam: undefined as number | undefined,
    queryFn: ({ pageParam }) => api.getAgentMemoryHistory(agentId, memoryId, pageParam),
    getNextPageParam: (page) => page.next_before_revision ?? undefined,
    refetchInterval: (query) => query.state.data?.pages.some((page) => page.versions.some((version) => version.expires_at && !version.expired)) ? 30_000 : false,
    enabled: Boolean(wsId && agentId && memoryId),
  });
}

export function agentMemoryUsageOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: [...agentMemoryKeys.list(wsId, agentId), "usage"],
    queryFn: () => api.getAgentMemoryUsage(agentId),
    enabled: Boolean(wsId && agentId),
    refetchInterval: 60_000,
  });
}

export function agentMemoryEvaluationsOptions(wsId: string, agentId: string, memoryId: string) {
  return queryOptions({ queryKey: [...agentMemoryKeys.list(wsId, agentId), "evaluations", memoryId], queryFn: () => api.listAgentMemoryEvaluations(agentId, memoryId), enabled: Boolean(wsId && agentId && memoryId), refetchInterval: (query) => query.state.data?.some((item) => item.execution_status === "queued" || item.execution_status === "running") ? 2_000 : 30_000 });
}

export function agentMemoryEvaluationOptions(wsId: string, agentId: string, memoryId: string, evaluationId: string) {
  return queryOptions({ queryKey: [...agentMemoryKeys.list(wsId, agentId), "evaluations", memoryId, evaluationId], queryFn: () => api.getAgentMemoryEvaluation(agentId, memoryId, evaluationId), enabled: Boolean(wsId && agentId && memoryId && evaluationId), refetchInterval: (query) => query.state.data?.execution_status === "queued" || query.state.data?.execution_status === "running" ? 2_000 : false });
}

export function useImportAgentMemoryEvaluation(wsId: string, agentId: string, memoryId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (report: unknown) => api.importAgentMemoryEvaluation(agentId, memoryId, report), onSettled: () => { qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) }); } });
}

export function useDeleteAgentMemoryEvaluation(wsId: string, agentId: string, memoryId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (id: string) => api.deleteAgentMemoryEvaluation(agentId, memoryId, id), onSettled: () => { qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) }); } });
}

export function memoryExecutionConfigOptions(wsId: string, agentId: string, memoryId: string, enabled: boolean) {
  return queryOptions({ queryKey: [...agentMemoryKeys.list(wsId, agentId), "execution-config", memoryId], queryFn: () => api.getMemoryExecutionConfig(agentId, memoryId), enabled: Boolean(enabled && wsId && agentId && memoryId), retry: false });
}
export function useStartMemoryExecution(wsId: string, agentId: string, memoryId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (request: MemoryExecutionRequest) => api.startMemoryExecution(agentId, memoryId, request), retry: false, onSettled: () => { qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) }); } });
}
export function useCancelMemoryExecution(wsId: string, agentId: string, memoryId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (id: string) => api.cancelMemoryExecution(agentId, memoryId, id), onSettled: () => { qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) }); } });
}
