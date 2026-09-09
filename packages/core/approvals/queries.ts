import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const approvalKeys = {
  all: (wsId: string) => ["approvals", wsId] as const,
  workspace: (wsId: string) => [...approvalKeys.all(wsId), "workspace"] as const,
  issue: (wsId: string, issueId: string) => [...approvalKeys.all(wsId), "issue", issueId] as const,
};

/** Every pending ask of the workspace; refreshed by approval:* events. */
export function workspaceApprovalsOptions(wsId: string) {
  return queryOptions({
    queryKey: approvalKeys.workspace(wsId),
    queryFn: () => api.listApprovals(),
    enabled: !!wsId,
    refetchInterval: 60_000,
  });
}

/** The pending asks of one issue, for the timeline. */
export function issueApprovalsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: approvalKeys.issue(wsId, issueId),
    queryFn: () => api.listApprovals(issueId),
    enabled: !!wsId && !!issueId,
  });
}
