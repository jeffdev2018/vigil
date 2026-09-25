/**
 * Inline approvals: cache keys + query options for the unified feed of
 * every pending human ask (Decision Cards, held status transitions, goal-
 * loop questions) — GET /api/approvals[?issue_id=].
 *
 * Key shape mirrors web's `packages/core/approvals/queries.ts`
 * (`["approvals", wsId, ...]`) so the cross-platform mental model stays the
 * same, but this file is mobile-owned rather than imported: web's
 * `workspaceApprovalsOptions` / `issueApprovalsOptions` call the web `api`
 * singleton (`../api`), and mobile owns its own (apps/mobile/data/api.ts),
 * per apps/mobile/CLAUDE.md ("Mobile shares only @multica/core types and
 * pure functions").
 */
import { useQuery, queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const approvalKeys = {
  all: (wsId: string | null) => ["approvals", wsId] as const,
  workspace: (wsId: string | null) =>
    [...approvalKeys.all(wsId), "workspace"] as const,
  issue: (wsId: string | null, issueId: string) =>
    [...approvalKeys.all(wsId), "issue", issueId] as const,
};

/** Every pending ask of the workspace; refreshed by approval:* events. */
export const workspaceApprovalsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: approvalKeys.workspace(wsId),
    queryFn: ({ signal }) => api.listApprovals(undefined, { signal }),
    enabled: !!wsId,
    refetchInterval: 60_000,
  });

/** The pending asks of one issue, for the timeline. */
export const issueApprovalsOptions = (wsId: string | null, issueId: string) =>
  queryOptions({
    queryKey: approvalKeys.issue(wsId, issueId),
    queryFn: ({ signal }) => api.listApprovals(issueId, { signal }),
    enabled: !!wsId && !!issueId,
  });

/** Every pending ask in the workspace — the inbox-level list/bar. */
export function useWorkspaceApprovals(wsId: string | null) {
  return useQuery(workspaceApprovalsOptions(wsId));
}

/** The pending asks of one issue — the timeline. */
export function useIssueApprovals(wsId: string | null, issueId: string) {
  return useQuery(issueApprovalsOptions(wsId, issueId));
}
