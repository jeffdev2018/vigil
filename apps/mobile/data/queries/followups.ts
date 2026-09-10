/**
 * Follow-ups (JEF-373) — the pending wake-ups of one issue's agent.
 *
 * Three-segment key shape per apps/mobile/CLAUDE.md ("Query / mutation
 * factory pattern"). Keyed per issue rather than per workspace because the
 * endpoint is issue-scoped and the only consumer is the issue's own
 * Follow-ups section; `.all(wsId)` still lets a workspace switch clear every
 * issue's list at once.
 *
 * Mirrored, not imported: web's factory (if/when it lands in
 * packages/core/followups/) is a different runtime instance — see
 * apps/mobile/CLAUDE.md "Mobile-owned updaters".
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const followupKeys = {
  all: (wsId: string | null) => ["followups", wsId] as const,
  issue: (wsId: string | null, issueId: string) =>
    [...followupKeys.all(wsId), "issue", issueId] as const,
};

export const issueFollowupsOptions = (wsId: string | null, issueId: string) =>
  queryOptions({
    queryKey: followupKeys.issue(wsId, issueId),
    queryFn: ({ signal }) => api.listIssueFollowups(issueId, { signal }),
    enabled: !!wsId && issueId.length > 0,
  });
