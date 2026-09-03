import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueDependencyType } from "../types";
import { issueKeys } from "./queries";

export function issueDependenciesOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueKeys.dependencies(wsId, issueId),
    queryFn: ({ signal }) => api.listIssueDependencies(issueId, { signal }),
  });
}

// Both mutations touch two issues, so they invalidate the workspace prefix
// rather than guessing which per-issue lists are mounted.
export function useAddIssueDependency(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { issueId: string; targetIssueId: string; type: IssueDependencyType }) =>
      api.createIssueDependency(v.issueId, { target_issue_id: v.targetIssueId, type: v.type }),
    onSettled: () => qc.invalidateQueries({ queryKey: issueKeys.dependenciesAll(wsId) }),
  });
}

export function useRemoveIssueDependency(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { issueId: string; dependencyId: string }) =>
      api.deleteIssueDependency(v.issueId, v.dependencyId),
    onSettled: () => qc.invalidateQueries({ queryKey: issueKeys.dependenciesAll(wsId) }),
  });
}
