import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateShareLinkInput } from "./schemas";

export const runPreviewKeys = {
  // Prefix-shaped: the realtime handler invalidates ["run-preview", wsId]
  // without knowing which run changed.
  preview: (wsId: string, taskId: string) => ["run-preview", wsId, taskId] as const,
  shareLinks: (wsId: string, taskId: string) => ["run-preview", wsId, taskId, "share-links"] as const,
};

export function runPreviewOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: runPreviewKeys.preview(wsId, taskId),
    queryFn: () => api.getRunPreview(taskId),
    enabled: !!wsId && !!taskId,
  });
}

export function useRunPreview(wsId: string, taskId: string) {
  return useQuery(runPreviewOptions(wsId, taskId));
}

export function useTaskShareLinks(wsId: string, taskId: string) {
  return useQuery({
    queryKey: runPreviewKeys.shareLinks(wsId, taskId),
    queryFn: () => api.listTaskShareLinks(taskId),
    enabled: !!wsId && !!taskId,
  });
}

/**
 * Never optimistic: the code is minted by the server, so there is nothing to
 * predict, and showing a link before it exists would invite copying one that
 * does not work.
 */
export function useCreateTaskShareLink(wsId: string, taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateShareLinkInput) => api.createTaskShareLink(taskId, input),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: runPreviewKeys.preview(wsId, taskId) });
    },
  });
}

export function useRevokeTaskShareLink(wsId: string, taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (linkId: string) => api.revokeTaskShareLink(taskId, linkId),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: runPreviewKeys.preview(wsId, taskId) });
    },
  });
}
