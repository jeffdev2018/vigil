import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { ArchivedInboxPage } from "../types/inbox";
import { EMPTY_INBOX_FILTERS, type InboxFilters } from "./filter-store";
import { infiniteQueryOptions, queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { InboxItem, InboxWorkspaceUnread, IssueDecision } from "../types";
import type { ApprovalGoalQuestion, ApprovalSource, ApprovalTransition } from "../approvals/schemas";

export const inboxKeys = {
  all: (wsId: string) => ["inbox", wsId] as const,
  list: (wsId: string) => [...inboxKeys.all(wsId), "list"] as const,
  archived: (wsId: string) => [...inboxKeys.all(wsId), "archived"] as const,
  attention: (wsId: string) => [...inboxKeys.all(wsId), "attention"] as const,
  briefing: (wsId: string) => [...inboxKeys.all(wsId), "briefing"] as const,
  // Inbox zero (K63): my pending asks, capped at five. The requested sources
  // (JEF-244 `include`) are part of the key so the plain call and a widened
  // one don't share a cache entry; the key keeps the "decisions" prefix so
  // the approval:asked/decided invalidation reaches both.
  decisions: (wsId: string, include?: string[]) =>
    include && include.length > 0
      ? ([...inboxKeys.all(wsId), "decisions", include.join(",")] as const)
      : ([...inboxKeys.all(wsId), "decisions"] as const),
  pages: (wsId: string) => [...inboxKeys.archived(wsId), "pages"] as const,
  lookup: (wsId: string) => [...inboxKeys.archived(wsId), "lookup"] as const,
  facets: (wsId: string) => [...inboxKeys.all(wsId), "archived-facets"] as const,
  // Account-level (not workspace-scoped): a single shared cache entry that
  // holds unread counts for every workspace the user belongs to.
  unreadSummary: () => ["inbox", "unread-summary"] as const,
};

export function inboxListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.list(wsId),
    queryFn: () => api.listInbox(),
  });
}

/**
 * @deprecated Legacy array endpoint, capped at 200 groups. New archive
 * consumers must use archivedInboxPagesOptions for the complete archive.
 * Retained for compatibility with legacy cache consumers.
 */
export function archivedInboxListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.archived(wsId),
    queryFn: () => api.listArchivedInbox(),
  });
}

function normalizedInboxFilters(filters: InboxFilters): InboxFilters {
  return { statuses: [...filters.statuses].sort(), priorities: [...filters.priorities].sort(),
    actors: [...filters.actors].sort(), unreadOnly: filters.unreadOnly };
}

export function archivedInboxPagesOptions(wsId: string, filters: InboxFilters) {
  return infiniteQueryOptions({
    queryKey: [...inboxKeys.pages(wsId), normalizedInboxFilters(filters)],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => api.listArchivedInboxPage(filters, { cursor: pageParam, signal }),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    retry: false,
  });
}

export function archivedInboxLookupOptions(wsId: string, groupId: string, filters: InboxFilters = EMPTY_INBOX_FILTERS) {
  return queryOptions({
    queryKey: [...inboxKeys.lookup(wsId), groupId, normalizedInboxFilters(filters)],
    queryFn: ({ signal }) => api.listArchivedInboxPage(filters, { groupId, signal }),
    enabled: !!groupId,
    retry: false,
  });
}

export function archivedInboxFacetsOptions(wsId: string, filters: InboxFilters) {
  return queryOptions({
    queryKey: [...inboxKeys.facets(wsId), normalizedInboxFilters(filters)],
    queryFn: ({ signal }) => api.getArchivedInboxFacets(filters, signal),
    retry: false,
  });
}

export type ArchivedInboxCache = InboxItem[] | ArchivedInboxPage | InfiniteData<ArchivedInboxPage>;

export function mapArchivedInboxCache(data: ArchivedInboxCache, patch: (items: InboxItem[]) => InboxItem[]): ArchivedInboxCache {
  if (Array.isArray(data)) return patch(data);
  if ("pages" in data) return { ...data, pages: data.pages.map((page) => ({ ...page, items: patch(page.items) })) };
  return { ...data, items: patch(data.items) };
}

export function patchArchivedInboxCaches(qc: QueryClient, wsId: string, patch: (items: InboxItem[]) => InboxItem[]) {
  const snapshot = qc.getQueriesData<ArchivedInboxCache>({ queryKey: inboxKeys.archived(wsId) });
  for (const [key, data] of snapshot) {
    if (data) qc.setQueryData(key, mapArchivedInboxCache(data, patch));
  }
  return snapshot;
}

/**
 * Cross-workspace unread inbox summary. One cache entry shared across all
 * workspaces — the data is account-level, so switching workspaces does not
 * refetch it; only the derived "is this for another workspace" view changes.
 */
export function inboxUnreadSummaryOptions() {
  return queryOptions({
    queryKey: inboxKeys.unreadSummary(),
    queryFn: () => api.getInboxUnreadSummary(),
  });
}

/**
 * Whether any workspace OTHER than `currentWsId` has unread inbox items.
 * Drives the workspace-switcher dot: the active workspace's own unread is
 * already surfaced by the Inbox nav count, so it is excluded here to avoid a
 * duplicate signal.
 */
export function hasOtherWorkspaceUnread(
  summary: InboxWorkspaceUnread[],
  currentWsId: string | null | undefined,
): boolean {
  return summary.some((s) => s.workspace_id !== currentWsId && s.count > 0);
}

/**
 * Set of workspace ids that have unread inbox items. Lets the workspace
 * switcher dropdown mark WHICH workspace a pending message lives in (the
 * aggregate switcher dot only says "somewhere else"). Workspaces with a zero
 * count are excluded.
 */
export function unreadWorkspaceIds(summary: InboxWorkspaceUnread[]): Set<string> {
  return new Set(summary.filter((s) => s.count > 0).map((s) => s.workspace_id));
}

/**
 * Unread inbox count for one workspace within the cross-workspace summary.
 * A workspace with nothing unread is absent from the response entirely, so a
 * missing entry means zero rather than "not loaded yet".
 */
export function unreadCountForWorkspace(
  summary: InboxWorkspaceUnread[],
  wsId: string | null | undefined,
): number {
  if (!wsId) return 0;
  return summary.find((s) => s.workspace_id === wsId)?.count ?? 0;
}

/**
 * Unread inbox count for the given workspace — the number the sidebar nav
 * badge and the desktop dock badge render.
 *
 * Read from the cross-workspace summary, NOT from the inbox list. The summary
 * is one small server-computed row per workspace and the sidebar already
 * fetches it for the workspace-switcher dot, so the badge costs no request of
 * its own; deriving it from `listInbox()` instead downloaded the entire
 * unbounded inbox on every app start just to render a number (MUL-6967).
 *
 * `GET /api/inbox/unread-count` is deliberately not the source: it counts raw
 * notification rows, while the inbox renders one row per issue. The summary
 * endpoint applies the same newest-per-issue rule `deduplicateInboxItems`
 * applies client-side, so this number matches the list the user sees.
 */
export function useInboxUnreadCount(wsId: string | null | undefined): number {
  const { data } = useQuery({
    ...inboxUnreadSummaryOptions(),
    enabled: !!wsId,
    select: (summary: InboxWorkspaceUnread[]) =>
      unreadCountForWorkspace(summary, wsId),
  });
  return data ?? 0;
}

/**
 * Deduplicate inbox items by issue_id (one entry per issue, Linear-style).
 * Exported for consumers to use in useMemo — not in queryOptions select
 * (to avoid new array references on every cache update).
 */
export function deduplicateInboxItems(items: InboxItem[]): InboxItem[] {
  return groupInboxItemsByIssue(items.filter((i) => !i.archived));
}

/**
 * Same grouping for the archived sub-view. The `archived` filter is what makes
 * an optimistic unarchive drop the row out of the archived list immediately —
 * exactly mirroring how `deduplicateInboxItems`' filter drops an optimistically
 * archived row out of the main list.
 */
export function deduplicateArchivedInboxItems(items: InboxItem[]): InboxItem[] {
  return groupInboxItemsByIssue(items.filter((i) => i.archived));
}

function groupInboxItemsByIssue(items: InboxItem[]): InboxItem[] {
  const groups = new Map<string, InboxItem[]>();
  for (const item of items) {
    const key = item.issue_id ?? item.id;
    const group = groups.get(key) ?? [];
    group.push(item);
    groups.set(key, group);
  }
  const merged: InboxItem[] = [];
  for (const group of groups.values()) {
    group.sort(
      (a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );
    const newest = group[0];
    if (!newest) continue;

    const commentId =
      newest.details?.comment_id ??
      group.find((item) => item.details?.comment_id)?.details?.comment_id;

    if (commentId && newest.details?.comment_id !== commentId) {
      merged.push({
        ...newest,
        details: { ...(newest.details ?? {}), comment_id: commentId },
      });
      continue;
    }

    merged.push(newest);
  }
  return merged.sort(
    (a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
}

// Inbox zero (K63): the asks waiting for me — Decision Cards, and with the
// JEF-244 `include` param also held status transitions and goal-loop
// questions — ordered and capped on the server (risk then deadline, five
// plus the total).
export interface InboxDecision {
  inbox_item_id: string;
  issue_id: string;
  issue_identifier: string;
  issue_title: string;
  risk_score: number;
  /** Which pending ask the entry carries; pre-JEF-244 servers omit it and every entry is a Decision Card. */
  source: ApprovalSource;
  /** Set iff source is "decision". */
  decision: IssueDecision | null;
  /** Set iff source is "transition" (a held status change). */
  transition: ApprovalTransition | null;
  /** Set iff source is "goal_question" (the goalstate.Question wire shape). */
  goal_question: ApprovalGoalQuestion | null;
}

export interface InboxDecisions {
  decisions: InboxDecision[];
  total: number;
}

export const inboxDecisionsOptions = (wsId: string, include?: string[]) =>
  queryOptions({
    queryKey: inboxKeys.decisions(wsId, include),
    queryFn: () => api.listInboxDecisions(include),
    enabled: wsId.length > 0,
    refetchInterval: 30_000,
  });

/** Attention Inbox (K02): human-only items, ordered by risk on the server. */
export const attentionInboxListOptions = (wsId: string) =>
  queryOptions({
    queryKey: inboxKeys.attention(wsId),
    queryFn: () => api.listAttentionInbox(),
    enabled: wsId.length > 0,
  });

// Morning briefing (K30): today's three sections, recomposed on read.
export function morningBriefingOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.briefing(wsId),
    queryFn: ({ signal }) => api.getMorningBriefingToday({ signal }),
    staleTime: 60_000,
  });
}

// Standup and retro (K34): the stored retro of a week (any day of it), or the latest.
export function weeklyRetroOptions(wsId: string, week?: string) {
  return queryOptions({
    queryKey: ["inbox", wsId, "retro", week ?? "latest"] as const,
    queryFn: () => api.getWeeklyRetro(week),
  });
}
