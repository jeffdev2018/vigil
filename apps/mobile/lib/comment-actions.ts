/**
 * Pure eligibility predicate for a comment-card action mobile is missing
 * relative to web (`packages/views/issues/components/comment-card.tsx`):
 * retrying a failed agent run. Kept standalone (rather than inlined at the
 * call site in comment-card.tsx) so the parity rule with web's source
 * function has one canonical, testable home.
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
