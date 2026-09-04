import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

// Module ownership (K33): rules live at workspace level; a suggestion is a
// read per issue. Assignment goes through the ordinary issue update.

export const ownershipKeys = {
  rules: (wsId: string) => ["module-ownership", wsId] as const,
};

export function moduleOwnershipOptions(wsId: string) {
  return queryOptions({
    queryKey: ownershipKeys.rules(wsId),
    queryFn: () => api.listModuleOwnership(),
  });
}

export function ownershipSuggestionOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: [...issueKeys.all(wsId), "ownership-suggestion", issueId] as const,
    queryFn: ({ signal }) => api.getOwnershipSuggestion(issueId, { signal }),
  });
}

export function useCreateModuleOwnership(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { path_pattern?: string; label_id?: string; owner_user_id: string; referent_agent_id?: string; priority?: number }) =>
      api.createModuleOwnership(v),
    onSettled: () => qc.invalidateQueries({ queryKey: ownershipKeys.rules(wsId) }),
  });
}

export function useDeleteModuleOwnership(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteModuleOwnership(id),
    onSettled: () => qc.invalidateQueries({ queryKey: ownershipKeys.rules(wsId) }),
  });
}
