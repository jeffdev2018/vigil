import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { SandboxPolicy } from "../types";

// Issue-level sandbox override (JEF-256): the issue half of the
// workspace < project < issue policy chain. Most-restrictive wins,
// enforced fail-closed by the daemon.

export const sandboxOverrideKeys = {
  issue: (wsId: string, issueId: string) => ["issues", wsId, "sandbox-override", issueId] as const,
};

export function issueSandboxOverrideOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: sandboxOverrideKeys.issue(wsId, issueId), queryFn: () => api.getIssueSandboxOverride(issueId), enabled: !!issueId });
}

export function usePutIssueSandboxOverride(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (policy: SandboxPolicy) => api.putIssueSandboxOverride(issueId, policy),
    onSettled: () => qc.invalidateQueries({ queryKey: sandboxOverrideKeys.issue(wsId, issueId) }),
  });
}

export function useDeleteIssueSandboxOverride(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteIssueSandboxOverride(issueId),
    onSettled: () => qc.invalidateQueries({ queryKey: sandboxOverrideKeys.issue(wsId, issueId) }),
  });
}
