/**
 * Pure eligibility predicates for the two comment-card actions mobile is
 * missing relative to web (`packages/views/issues/components/comment-card.tsx`):
 * retrying a failed agent run, and creating a sub-issue anchored on a
 * comment. Kept as standalone predicates (rather than inlined at the call
 * site in comment-context-menu.tsx) so the parity rule with web's source
 * functions has one canonical, testable home.
 */
import type { TimelineEntry } from "@multica/core/types";

/**
 * Mirrors web's `retryableAgentFailureComment`
 * (packages/views/issues/components/comment-card.tsx:242-249): a system
 * comment posted by an agent that names the task it failed in is eligible
 * for a one-tap rerun.
 */
export function retryableAgentFailureComment(
  entry: TimelineEntry,
): entry is TimelineEntry & { source_task_id: string } {
  return (
    entry.actor_type === "agent" &&
    entry.comment_type === "system" &&
    typeof entry.source_task_id === "string" &&
    entry.source_task_id.length > 0
  );
}

/**
 * Mirrors web's sub-issue eligibility check
 * (packages/views/issues/components/comment-card.tsx:701, 1052): only an
 * ordinary comment — not a system/quick-action entry — can anchor a
 * captured sub-issue.
 */
export function canCreateSubIssueFromComment(entry: TimelineEntry): boolean {
  return entry.comment_type === "comment";
}
