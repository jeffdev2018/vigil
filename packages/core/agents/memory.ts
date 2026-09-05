import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { AgentMemoryState } from "../types";

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
    enabled: Boolean(agentId),
  });
}

export function useCreateAgentMemory(agentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (content: string) => api.createAgentMemory(agentId, content),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) });
    },
  });
}

export function useUpdateAgentMemory(agentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      memoryId,
      content,
      state,
    }: {
      memoryId: string;
      content?: string;
      state?: AgentMemoryState;
    }) => api.updateAgentMemory(agentId, memoryId, { content, state }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) });
    },
  });
}

// No optimistic removal: the row disappears only after the server confirms,
// matching the labels delete pattern.
export function useDeleteAgentMemory(agentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (memoryId: string) => api.deleteAgentMemory(agentId, memoryId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentMemoryKeys.list(wsId, agentId) });
    },
  });
}
