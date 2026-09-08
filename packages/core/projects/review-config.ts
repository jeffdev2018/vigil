import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { UpdateProjectReviewConfigRequest } from "../types";

// Per-project agent review configuration (JEF-238): checklist, fixed
// reviewer, done-gate and rework-cycle cap.

export const projectReviewConfigKeys = {
  config: (wsId: string, projectId: string) => ["project-review-config", wsId, projectId] as const,
};

export function projectReviewConfigOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectReviewConfigKeys.config(wsId, projectId),
    queryFn: () => api.getProjectReviewConfig(projectId),
    enabled: !!projectId,
  });
}

export function useSaveProjectReviewConfig(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: UpdateProjectReviewConfigRequest) => api.putProjectReviewConfig(projectId, v),
    onSettled: () => qc.invalidateQueries({ queryKey: projectReviewConfigKeys.config(wsId, projectId) }),
  });
}
