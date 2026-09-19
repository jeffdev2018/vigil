import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Follow-ups (OS plan, vague B, "réveil programmé"): a deferred wake-up of an
 * issue's agent — "come back to this at 9 tomorrow with this note". The list
 * carries the workspace's per-day budget alongside the pending rows, so the
 * dialog can quote the cap without a second request.
 */
export const followupKeys = {
  /** PREFIX: every follow-up query in the workspace. */
  all: (wsId: string) => ["followups", wsId] as const,
  issue: (wsId: string, issueId: string) => [...followupKeys.all(wsId), issueId] as const,
};

export function issueFollowupsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: followupKeys.issue(wsId, issueId),
    queryFn: ({ signal }) => api.listIssueFollowups(issueId, { signal }),
    enabled: issueId !== "",
  });
}
