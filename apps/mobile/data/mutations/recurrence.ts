/**
 * Recurrence writes (OS plan, table stakes) — set, toggle and stop the rule
 * of an issue's series.
 *
 * Nothing here is optimistic. The server owns the outcome: it recomputes
 * `next_run_at` from the cron in its timezone, re-lists the occurrences and
 * hands back three preview runs — none of which is locally predictable,
 * which is the first condition of the optimistic gate in the root CLAUDE.md.
 * "Stop recurring" is a confirm flow behind a native Alert, and confirm flows
 * await the server.
 *
 * Invalidation covers three surfaces, because one rule is shared by a whole
 * series:
 *   1. the issue the write was made from,
 *   2. every occurrence the payload names — each caches the SAME rule under
 *      its own key, so without this the user opens ONE-13 and reads the rule
 *      as it was before they edited it on ONE-12,
 *   3. the issues list — enabling a rule can spawn an occurrence, and
 *      clearing one turns past occurrences into ordinary issues.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  IssueRecurrenceResponse,
  SetIssueRecurrenceInput,
} from "@multica/core/types";
import { api } from "@/data/api";
import { issueKeys } from "@/data/queries/issue-keys";
import { recurrenceKeys } from "@/data/queries/recurrence";
import { useWorkspaceStore } from "@/data/workspace-store";
import type { QueryClient } from "@tanstack/react-query";

/**
 * Every key that holds this rule: the issue we wrote from, the series source,
 * and each occurrence the server listed. Exported so a future caller (a
 * series screen) invalidates the same surface instead of re-deriving it.
 */
export function invalidateRecurrenceSeries(
  qc: QueryClient,
  wsId: string | null,
  issueId: string,
  payload: IssueRecurrenceResponse | null | undefined,
) {
  const ids = new Set<string>([issueId]);
  if (payload?.source.id) ids.add(payload.source.id);
  for (const occurrence of payload?.occurrences ?? []) {
    if (occurrence.id) ids.add(occurrence.id);
  }
  for (const id of ids) {
    qc.invalidateQueries({ queryKey: recurrenceKeys.issue(wsId, id) });
  }
  // The issue lists only — enabling a rule can spawn an occurrence, which is
  // a new row on both. Deliberately NOT `issueKeys.all(wsId)`: that prefix
  // also covers every cached detail, timeline, task and attachment list, and
  // refetching all of them over cellular to learn about one new row is
  // exactly what the "patch over invalidate" rule in apps/mobile/CLAUDE.md
  // exists to prevent. The occurrence also arrives on its own as
  // `issue:created`.
  qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
}

export function useSetIssueRecurrence(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: (input: SetIssueRecurrenceInput) =>
      api.setIssueRecurrence(issueId, input),
    onSuccess: (payload) => {
      // The PUT answers with the whole payload, so seed the key we came from
      // before the invalidate refetches it — the section behind the sheet
      // repaints on the new rule instead of on the old one for a round-trip.
      qc.setQueryData(recurrenceKeys.issue(wsId, issueId), payload);
    },
    onSettled: (payload) => {
      invalidateRecurrenceSeries(qc, wsId, issueId, payload);
    },
  });
}

export function useClearIssueRecurrence(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: async () => {
      // Read the series membership BEFORE the delete: once the rule is gone
      // the GET 404s and there is nothing left to tell us which other issues
      // were caching it.
      const known = qc.getQueryData<IssueRecurrenceResponse | null>(
        recurrenceKeys.issue(wsId, issueId),
      );
      await api.clearIssueRecurrence(issueId);
      return known ?? null;
    },
    onSettled: (known) => {
      invalidateRecurrenceSeries(qc, wsId, issueId, known);
    },
  });
}
