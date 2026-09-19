import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { SandboxPolicy } from "../types";

// Sandbox policies (JEF-256): per-project network / sensitive-file
// restrictions on agent runs. Resolved workspace < project < issue,
// most-restrictive wins, enforced fail-closed by the daemon.

export const sandboxPolicyKeys = {
  project: (wsId: string, projectId: string) => ["sandbox-policy", wsId, projectId] as const,
};

export function projectSandboxPolicyOptions(wsId: string, projectId: string) {
  return queryOptions({ queryKey: sandboxPolicyKeys.project(wsId, projectId), queryFn: () => api.getProjectSandboxPolicy(projectId), enabled: !!projectId });
}

export function usePutProjectSandboxPolicy(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (policy: SandboxPolicy) => api.putProjectSandboxPolicy(projectId, policy),
    onSettled: () => qc.invalidateQueries({ queryKey: sandboxPolicyKeys.project(wsId, projectId) }),
  });
}

export function useDeleteProjectSandboxPolicy(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteProjectSandboxPolicy(projectId),
    onSettled: () => qc.invalidateQueries({ queryKey: sandboxPolicyKeys.project(wsId, projectId) }),
  });
}
