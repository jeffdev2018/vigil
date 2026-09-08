import { infiniteQueryOptions, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";

export function projectMemoryOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: [...projectKeys.detail(wsId, projectId), "memory"],
    queryFn: () => api.getProjectMemory(projectId),
    enabled: Boolean(wsId && projectId),
    refetchInterval: (query) => query.state.data?.expires_at && !query.state.data.expired ? 30_000 : false,
  });
}

export function useUpdateProjectMemory(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      rules,
      expectedRevision,
      expiresAt,
      sourceReviewId,
    }: {
      rules: string[];
      expectedRevision: number;
      expiresAt?: string | null;
      sourceReviewId?: string;
    }) =>
      api.updateProjectMemory(projectId, rules, expectedRevision, expiresAt, sourceReviewId),
    onSettled: () => qc.invalidateQueries({ queryKey: projectKeys.all(wsId) }),
  });
}

export function projectMemoryHistoryOptions(wsId: string, projectId: string) {
  return infiniteQueryOptions({
    queryKey: [...projectKeys.detail(wsId, projectId), "memory-history"],
    initialPageParam: undefined as number | undefined,
    queryFn: ({ pageParam }) => api.getProjectMemoryHistory(projectId, pageParam),
    getNextPageParam: (page) => page.next_before_revision ?? undefined,
    enabled: Boolean(wsId && projectId),
  });
}

export function projectMemoryUsageOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: [...projectKeys.detail(wsId, projectId), "memory-usage"],
    queryFn: () => api.getProjectMemoryUsage(projectId),
    enabled: Boolean(wsId && projectId),
    refetchInterval: 60_000,
  });
}

export function useRestoreProjectMemory(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ revision, expectedRevision }: { revision: number; expectedRevision: number }) =>
      api.restoreProjectMemory(projectId, revision, expectedRevision),
    onSettled: () => qc.invalidateQueries({ queryKey: projectKeys.all(wsId) }),
  });
}
