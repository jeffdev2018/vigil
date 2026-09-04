import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Blast radius (K07): per-project autonomy by path pattern.

export const blastRadiusKeys = {
  rules: (wsId: string, projectId: string) => ["blast-radius-rules", wsId, projectId] as const,
  preview: (wsId: string, projectId: string, path: string) => ["blast-radius-rules", wsId, projectId, "preview", path] as const,
};

export function blastRadiusRulesOptions(wsId: string, projectId: string) {
  return queryOptions({ queryKey: blastRadiusKeys.rules(wsId, projectId), queryFn: () => api.listBlastRadiusRules(projectId), enabled: !!projectId });
}

export function blastRadiusPreviewOptions(wsId: string, projectId: string, path: string) {
  const p = path.trim();
  return queryOptions({ queryKey: blastRadiusKeys.preview(wsId, projectId, p), queryFn: () => api.previewBlastRadius(projectId, p), enabled: !!projectId && p.length > 0, staleTime: 10_000 });
}

export function useCreateBlastRadiusRule(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { path_pattern: string; autonomy_level: string }) => api.createBlastRadiusRule(projectId, v),
    onSettled: () => qc.invalidateQueries({ queryKey: blastRadiusKeys.rules(wsId, projectId) }),
  });
}

export function useDeleteBlastRadiusRule(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ruleId: string) => api.deleteBlastRadiusRule(projectId, ruleId),
    onSettled: () => qc.invalidateQueries({ queryKey: blastRadiusKeys.rules(wsId, projectId) }),
  });
}
