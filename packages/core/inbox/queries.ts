import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { InboxItem, InboxWorkspaceUnread, IssueDecision } from "../types";

export const inboxKeys = {
  all: (wsId: string) => ["inbox", wsId] as const,
  list: (wsId: string) => [...inboxKeys.all(wsId), "list"] as const,
  archived: (wsId: string) => [...inboxKeys.all(wsId), "archived"] as const,
  attention: (wsId: string) => [...inboxKeys.all(wsId), "attention"] as const,
  briefing: (wsId: string) => [...inboxKeys.all(wsId), "briefing"] as const,
  // Inbox zero (K63): my pending Decision Cards, capped at five.
  decisions: (wsId: string) => [...inboxKeys.all(wsId), "decisions"] as const,
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
 * Archived notifications, backing the inbox's "Archived" sub-view. A separate
 * cache entry from the main list rather than one flat cache split locally
 * (which is what chat does): the archive grows without end, so it is fetched
 * from its own capped endpoint, and the server — not the client — decides
 * which issues belong in which list.
 */
export function archivedInboxListOptions(wsId: string) {
  return queryOptions({
    queryKey: inboxKeys.archived(wsId),
    queryFn: () => api.listArchivedInbox(),
  });
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
 * Unread inbox count for the given workspace, aligned with what the inbox
 * list UI renders: archived items excluded, then deduplicated by issue so a
 * single issue with three unread notifications counts once.
 */
export function useInboxUnreadCount(wsId: string | null | undefined): number {
  const { data } = useQuery({
    queryKey: inboxKeys.list(wsId ?? ""),
    queryFn: () => api.listInbox(),
    enabled: !!wsId,
    select: (items: InboxItem[]) =>
      deduplicateInboxItems(items).filter((i) => !i.read).length,
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

// Inbox zero (K63): the cards waiting for me, options included, ordered and
// capped on the server (risk then deadline, five plus the total).
export interface InboxDecision {
  inbox_item_id: string;
  issue_id: string;
  issue_identifier: string;
  issue_title: string;
  risk_score: number;
  decision: IssueDecision;
}

export interface InboxDecisions {
  decisions: InboxDecision[];
  total: number;
}

export const inboxDecisionsOptions = (wsId: string) =>
  queryOptions({
    queryKey: inboxKeys.decisions(wsId),
    queryFn: () => api.listInboxDecisions(),
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
