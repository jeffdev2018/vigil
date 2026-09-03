import type { MergeBlocker, MergeReadiness } from "../types";

/** Blocker kinds this build knows how to label. */
export const KNOWN_MERGE_BLOCKER_KINDS = [
  "checks_failing",
  "checks_pending",
  "merge_conflict",
  "merge_not_clean",
  "stale_snapshot",
  "unresolved_threads",
  "open_todos",
  "blocking_issue",
  "no_pr",
] as const;

export type KnownMergeBlockerKind = (typeof KNOWN_MERGE_BLOCKER_KINDS)[number];

export function isKnownMergeBlockerKind(kind: string): kind is KnownMergeBlockerKind {
  return (KNOWN_MERGE_BLOCKER_KINDS as readonly string[]).includes(kind);
}

/**
 * The readiness the UI may show as green. A server can only say "ready" with
 * no blockers, and a blocker kind this build does not know is still a
 * blocker: the answer is never true by default or by ignorance.
 */
export function effectiveMergeReady(readiness: Pick<MergeReadiness, "ready" | "blockers">): boolean {
  if (readiness.ready !== true) return false;
  return readiness.blockers.length === 0;
}

/** Blockers with an unknown kind, so the UI can render them generically. */
export function unknownMergeBlockers(blockers: MergeBlocker[]): MergeBlocker[] {
  return blockers.filter((b) => !isKnownMergeBlockerKind(b.kind));
}
