import type { TimelineEntry } from "@multica/core/types";
import { sortTimelineEntriesAsc } from "@multica/core/issues/timeline-sort";
import { preprocessMentionShortcodes } from "@multica/core/markdown";

/**
 * Walks the parent_id graph rooted at `rootId` and returns every descendant in
 * CHRONOLOGICAL order (created_at ASC, id tie-break). Shared between
 * CommentCard (which renders the expanded thread) and ResolvedThreadBar
 * (which displays the collapsed count + author list) so the two views stay in
 * sync — direct-children-only counts diverge once nested replies exist (see
 * Emacs review on PR #2300).
 *
 * Chronological, not depth-first: agent replies are forced to nest under the
 * comment that triggered them, so a depth-first walk lets a slow agent's late
 * reply render BEFORE earlier sibling replies (#3691). The server's --thread
 * output the agent reads is already chronological (ListThreadCommentsForIssue
 * in comment.sql); this keeps the UI on the same order.
 */
export function collectThreadReplies(
  rootId: string,
  repliesByParent: Map<string, TimelineEntry[]>,
): TimelineEntry[] {
  const out: TimelineEntry[] = [];
  const walk = (id: string) => {
    const children = repliesByParent.get(id) ?? [];
    for (const child of children) {
      out.push(child);
      walk(child.id);
    }
  };
  walk(rootId);
  return sortTimelineEntriesAsc(out);
}

/** Unique member and agent authors, root first, followed by all nested replies. */
export function collectThreadParticipants(
  root: TimelineEntry,
  replies: readonly TimelineEntry[],
): TimelineEntry[] {
  const seen = new Set<string>();
  return [root, ...replies].filter((entry) => {
    if (entry.actor_type !== "member" && entry.actor_type !== "agent") return false;
    const key = `${entry.actor_type}:${entry.actor_id}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/**
 * A thread's resolution, derived purely from `resolved_at`. Two user actions
 * write the same field:
 *   - "Resolve thread" sets resolved_at on the ROOT → whole thread folds.
 *   - "Resolve thread with comment" sets resolved_at on a REPLY → that reply is
 *     the resolution; the others fold around it.
 *
 * The derivation is total so the UI never shows two resolutions and never
 * crashes on any combination (older / concurrent writes can resolve more than
 * one): root wins; otherwise the reply with the latest resolved_at is THE
 * resolution. No write-side "clear the others" is needed — display picks one.
 */
export type ThreadResolution =
  | { kind: "none" }
  | { kind: "root" }
  | { kind: "reply"; resolutionId: string };

export function deriveThreadResolution(
  root: TimelineEntry,
  replies: TimelineEntry[],
): ThreadResolution {
  if (root.resolved_at) return { kind: "root" };
  let chosen: TimelineEntry | null = null;
  for (const reply of replies) {
    if (!reply.resolved_at) continue;
    if (!chosen || reply.resolved_at > chosen.resolved_at!) chosen = reply;
  }
  return chosen ? { kind: "reply", resolutionId: chosen.id } : { kind: "none" };
}

/**
 * IDs of every thread root (top-level comment) in a timeline — the units the
 * per-comment collapse store folds. Same root/reply split as issue-detail's
 * `timelineView` grouping: a comment is a root iff it has no `parent_id`.
 */
export function rootCommentIds(entries: readonly TimelineEntry[]): string[] {
  return entries
    .filter((e) => e.type === "comment" && !e.parent_id)
    .map((e) => e.id);
}

/**
 * IDs of thread roots that carry a resolution (on the root itself or on a
 * reply) — the threads that render folded behind a bar until expanded via
 * `useResolvedExpandStore`. Unresolved roots are excluded on purpose: seeding
 * them into the expand set would keep them expanded through a later resolve.
 */
export function resolvedThreadRootIds(entries: readonly TimelineEntry[]): string[] {
  const roots: TimelineEntry[] = [];
  const repliesByParent = new Map<string, TimelineEntry[]>();
  for (const e of entries) {
    if (e.type !== "comment") continue;
    if (!e.parent_id) {
      roots.push(e);
    } else {
      const list = repliesByParent.get(e.parent_id) ?? [];
      list.push(e);
      repliesByParent.set(e.parent_id, list);
    }
  }
  return roots
    .filter(
      (root) =>
        deriveThreadResolution(root, collectThreadReplies(root.id, repliesByParent)).kind !==
        "none",
    )
    .map((root) => root.id);
}

/** Which slice of the outline the reader is looking at. */
export type ThreadOutlineFilter = "all" | "unresolved" | "resolved" | "mine";

/** The fields the outline filters read — a subset of ThreadMinimapThread. */
export interface FilterableThread {
  resolved: boolean;
  /** The reader authored, replied in, or was @mentioned in this thread. */
  involvesMe: boolean;
}

export function matchesThreadFilter(
  thread: FilterableThread,
  filter: ThreadOutlineFilter,
): boolean {
  switch (filter) {
    case "unresolved":
      return !thread.resolved;
    case "resolved":
      return thread.resolved;
    case "mine":
      return thread.involvesMe;
    case "all":
      return true;
    default:
      // The union covers every pill, but an exhaustive default keeps a future
      // filter from silently hiding the whole outline.
      return true;
  }
}

/**
 * Whether `content` @mentions `userId`.
 *
 * Current mentions serialize as markdown links to `mention://member/<uuid>`,
 * but the legacy `[@ id="..." label="..."]` shortcode form is still in the
 * database — `preprocessMarkdown` migrates it on read, not before storage, and
 * the timeline hands us raw content. Matching only the link form would drop
 * every thread whose sole mention of the reader was written in the old format.
 * `preprocessMentionShortcodes` returns its input untouched when there is no
 * `[@ ` in it, so the normal path costs one substring scan.
 */
export function mentionsUser(content: string | undefined, userId: string): boolean {
  if (!content || !userId) return false;
  return preprocessMentionShortcodes(content).includes(`mention://member/${userId}`);
}

/**
 * "@me" means the thread concerns this reader: they started it, answered in
 * it, or were @mentioned anywhere in it. Authorship counts because a thread
 * you spoke in is one you are expected to follow — narrowing to literal
 * mentions would drop most of them.
 */
export function threadInvolvesUser(
  root: TimelineEntry,
  replies: readonly TimelineEntry[],
  userId: string,
): boolean {
  if (!userId) return false;
  return [root, ...replies].some(
    (entry) =>
      (entry.actor_type === "member" && entry.actor_id === userId) ||
      mentionsUser(entry.content, userId),
  );
}
