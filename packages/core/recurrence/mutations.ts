import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import type { IssueRecurrenceResponse, SetIssueRecurrenceInput } from "../types";
import { recurrenceKeys } from "./queries";

/**
 * Nothing here is optimistic. The server owns the resolved next runs (a cron in
 * a timezone is not locally predictable), the PUT can be refused (400 on a bad
 * expression, 403 for a run), and a clear detaches every occurrence — an
 * outcome no local patch can fake. Both await the server, then invalidate.
 *
 * The rule is shared by the whole series, so a change on one member stales the
 * block on every other member that is already in cache; and a new occurrence
 * or a detached one changes what the issue list shows.
 */
export function invalidateIssueRecurrence(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  seriesIds: readonly string[] = [],
): void {
  for (const id of new Set([issueId, ...seriesIds].filter((v) => v !== ""))) {
    qc.invalidateQueries({ queryKey: recurrenceKeys.issue(wsId, id) });
  }
  qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
}

/** The series members held by a payload: its source and its occurrences. */
export function recurrenceSeriesIds(payload: IssueRecurrenceResponse | null | undefined): string[] {
  if (!payload) return [];
  return [payload.source.id, ...payload.occurrences.map((o) => o.id)].filter((id) => id !== "");
}

export function useSetIssueRecurrence(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SetIssueRecurrenceInput) => api.setIssueRecurrence(issueId, input),
    onSettled: (data) => {
      // The answer names the whole series; on a failure the ids still in cache
      // do, which is why the read happens here rather than only on success.
      const ids = data ? recurrenceSeriesIds(data) : recurrenceSeriesIds(cached(qc, wsId, issueId));
      invalidateIssueRecurrence(qc, wsId, issueId, ids);
    },
  });
}

export function useClearIssueRecurrence(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    // The DELETE answers 204, so the series has to be read before it is gone.
    mutationFn: async () => {
      const ids = recurrenceSeriesIds(cached(qc, wsId, issueId));
      await api.clearIssueRecurrence(issueId);
      return ids;
    },
    onSettled: (ids) => invalidateIssueRecurrence(qc, wsId, issueId, ids ?? []),
  });
}

function cached(qc: QueryClient, wsId: string, issueId: string): IssueRecurrenceResponse | null {
  return qc.getQueryData<IssueRecurrenceResponse | null>(recurrenceKeys.issue(wsId, issueId)) ?? null;
}
