import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueRecurrenceResponse } from "../types";

/**
 * Recurring issues (OS plan, table stakes): a standing order filed on an issue
 * — "raise this again every Monday at 09:00". The rule belongs to the series,
 * so the source and every occurrence read the same payload.
 *
 * `null` is "this issue does not recur", not a failure: the endpoint 404s and
 * the client turns that into `null` (see api/client.ts), so the block renders
 * its empty state instead of an error.
 */
export const recurrenceKeys = {
  /** PREFIX: every recurrence query in the workspace. */
  all: (wsId: string) => ["issue-recurrence", wsId] as const,
  issue: (wsId: string, issueId: string) => [...recurrenceKeys.all(wsId), issueId] as const,
};

export function issueRecurrenceOptions(wsId: string, issueId: string) {
  return queryOptions<IssueRecurrenceResponse | null>({
    queryKey: recurrenceKeys.issue(wsId, issueId),
    queryFn: ({ signal }) => api.getIssueRecurrence(issueId, { signal }),
    enabled: issueId !== "",
  });
}

export function useIssueRecurrence(wsId: string, issueId: string) {
  return useQuery(issueRecurrenceOptions(wsId, issueId));
}
