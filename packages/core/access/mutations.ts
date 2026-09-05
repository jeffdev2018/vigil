import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { ProjectRole, SSOConnectionRequest } from "./schemas";
import { accessKeys } from "./queries";

export function usePutSSOConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (data: SSOConnectionRequest) => api.putSSOConnection(wsId, data), onSettled: () => qc.invalidateQueries({ queryKey: accessKeys.sso(wsId) }) });
}

export function useSetSSOEnforced(wsId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (enforced: boolean) => api.setSSOEnforced(wsId, enforced), onSettled: () => qc.invalidateQueries({ queryKey: accessKeys.sso(wsId) }) });
}

export function useDeleteSSOConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: () => api.deleteSSOConnection(wsId), onSettled: () => qc.invalidateQueries({ queryKey: accessKeys.sso(wsId) }) });
}

export function useCreateScimToken(wsId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: () => api.createScimToken(wsId), onSettled: () => qc.invalidateQueries({ queryKey: accessKeys.scimTokens(wsId) }) });
}

export function useDeleteScimToken(wsId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (tokenId: string) => api.deleteScimToken(wsId, tokenId), onSettled: () => qc.invalidateQueries({ queryKey: accessKeys.scimTokens(wsId) }) });
}

export function useSetProjectMemberRole(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ subjectType, subjectId, role }: { subjectType: "member" | "agent"; subjectId: string; role: ProjectRole | null }) =>
      role === null ? api.clearProjectMemberRole(projectId, subjectType, subjectId) : api.setProjectMemberRole(projectId, subjectType, subjectId, role),
    onSettled: () => qc.invalidateQueries({ queryKey: accessKeys.projectMembers(wsId, projectId) }),
  });
}
