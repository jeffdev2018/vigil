import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { runKeys } from "../runs/fleet-queries";
import type { ScheduleFollowupInput } from "../types";
import { followupKeys } from "./queries";

/**
 * Nothing here is optimistic. Scheduling can be refused by the daily budget
 * (429) and the server owns the resolved instant ("+90" becomes a real
 * fires_at); cancelling can lose a race with the scheduler (409). Both await
 * the server, then invalidate — CLAUDE.md's rule for outcomes that are not
 * locally predictable.
 *
 * A follow-up is a deferred run, so the fleet list moves with it.
 */
export function invalidateFollowups(qc: QueryClient, wsId: string, issueId: string): void {
  qc.invalidateQueries({ queryKey: followupKeys.issue(wsId, issueId) });
  qc.invalidateQueries({ queryKey: runKeys.all(wsId) });
}

export function useScheduleFollowup(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ScheduleFollowupInput) => api.scheduleIssueFollowup(issueId, input),
    onSettled: () => invalidateFollowups(qc, wsId, issueId),
  });
}

export function useCancelFollowup(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (followupId: string) => api.cancelIssueFollowup(issueId, followupId),
    onSettled: () => invalidateFollowups(qc, wsId, issueId),
  });
}
