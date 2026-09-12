/**
 * Recurring issues (OS plan, table stakes) — the rule of the series an issue
 * belongs to.
 *
 * Three-segment key shape per apps/mobile/CLAUDE.md ("Query / mutation
 * factory pattern"). Keyed per issue, like follow-ups, because the endpoint
 * is issue-scoped; `.all(wsId)` still clears every issue's rule at once on a
 * workspace switch.
 *
 * One rule is shared by the whole series, so the SAME rule is cached under as
 * many keys as the user has opened issues of that series. That is why every
 * write invalidates each occurrence's key, not just the one it was made from
 * (see data/mutations/recurrence.ts).
 *
 * Mirrored, not imported: web's factory is a different runtime instance — see
 * apps/mobile/CLAUDE.md "Mobile-owned updaters".
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const recurrenceKeys = {
  all: (wsId: string | null) => ["issue-recurrence", wsId] as const,
  issue: (wsId: string | null, issueId: string) =>
    [...recurrenceKeys.all(wsId), "issue", issueId] as const,
};

export const issueRecurrenceOptions = (wsId: string | null, issueId: string) =>
  queryOptions({
    queryKey: recurrenceKeys.issue(wsId, issueId),
    queryFn: ({ signal }) => api.getIssueRecurrence(issueId, { signal }),
    enabled: !!wsId && issueId.length > 0,
  });
