import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { mirrorKeys } from "./queries";

export function useCreateMirrorLink(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ targetProjectId, triggerLabel }: { targetProjectId: string; triggerLabel: string }) =>
      api.createMirrorLink(projectId, targetProjectId, triggerLabel),
    onSettled: () => qc.invalidateQueries({ queryKey: mirrorKeys.links(wsId, projectId) }),
  });
}

/** Deleting a link keeps the mirrors it already produced, so only the list changes. */
export function useDeleteMirrorLink(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (linkId: string) => api.deleteMirrorLink(projectId, linkId),
    onSettled: () => qc.invalidateQueries({ queryKey: mirrorKeys.links(wsId, projectId) }),
  });
}

export function useSetMirrorTypeSynced(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ mirrorId, value }: { mirrorId: string; value: boolean }) =>
      api.setMirrorTypeSynced(issueId, mirrorId, value),
    onSettled: () => qc.invalidateQueries({ queryKey: mirrorKeys.issueMirrors(wsId, issueId) }),
  });
}
